package collector

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"alphaflow/go-service/market-data/internal/aggregator"
	"alphaflow/go-service/market-data/internal/backfillqueue"
	"alphaflow/go-service/market-data/internal/exchange"
	"alphaflow/go-service/market-data/internal/model"
)

type Store interface {
	LastOpenTime(
		ctx context.Context,
		exchange string,
		market string,
		symbol string,
		interval string,
	) (int64, bool, error)
	RangeKlines(ctx context.Context, exchange string, market string, symbol string, interval string, start int64, end int64) ([]model.Kline, error)
	UpsertKline(ctx context.Context, kline model.Kline) error
	UpsertKlines(ctx context.Context, klines []model.Kline) error
	SetLastPrice(ctx context.Context, price model.LastPrice) error
	SetMarkPrice(ctx context.Context, price model.MarkPrice) error
	SetBookTicker(ctx context.Context, ticker model.BookTicker) error
	SetOpenInterest(ctx context.Context, interest model.OpenInterest) error
	AddLiquidation(ctx context.Context, liquidation model.Liquidation, limit int64) error
	SetMarketStatus(ctx context.Context, status model.MarketStatus) error
	SetWebSocketStatus(ctx context.Context, status model.WebSocketStatus) error
}

type GapPublisher interface {
	Publish(context.Context, backfillqueue.Task) (string, error)
}

const (
	maxReconnectDelay        = time.Minute
	reconnectBackoffReset    = time.Minute
	defaultReconnectDelay    = 5 * time.Second
	defaultMaxStreams        = 100
	minEventQueueSize        = 10000
	maxEventQueueSize        = 100000
	defaultEventWorkers      = 4
	maxEventWorkers          = 16
	latestEventEnqueueTTL    = 50 * time.Millisecond
	latestFlushInterval      = 500 * time.Millisecond
	defaultEventDrainTimeout = 10 * time.Second
	backfillRequestInterval  = 100 * time.Millisecond
	openInterestStartupDelay = 2 * time.Minute
	maxOpenInterestSymbols   = 100
)

var backfillIntervalPriority = []string{"1m", "5m", "3m", "15m", "30m", "1h", "2h", "4h"}

type Collector struct {
	options             Options
	rest                exchange.RESTClient
	ws                  exchange.WSClient
	store               Store
	events              eventState
	klines              klineState
	now                 func() time.Time
	lastBackfillRequest time.Time
}

type eventState struct {
	queue             chan collectorEvent
	workers           int
	pending           atomic.Int64
	drainNotify       chan struct{}
	latestMu          sync.Mutex
	latest            map[string]collectorEvent
	stats             collectorStats
	timingMu          sync.Mutex
	lastExchangeTimes map[string]int64
}

type klineState struct {
	versionMu           sync.Mutex
	versions            map[string]map[int64]klineVersion
	writeLocks          [256]sync.Mutex
	continuityMu        sync.Mutex
	lastClosedOpenTimes map[string]int64
	pendingGaps         map[string]map[string]klineGap
}

type Stats struct {
	ProcessedEvents       uint64
	DroppedLatestEvents   uint64
	CoalescedLatestEvents uint64
	FlushedLatestEvents   uint64
	ProcessEventErrors    uint64
	QueueLen              int
	QueueCap              int
	QueuePeak             int64
	LastEventReceivedAt   int64
	LastEventProcessedAt  int64
	SourceDelayMaxMillis  int64
	QueueDelayMaxMillis   int64
	ProcessMaxMillis      int64
	OutOfOrderEvents      uint64
	DuplicateKlineEvents  uint64
	StaleKlineEvents      uint64
	OpenAfterClosedEvents uint64
	WebSocketKlineEvents  uint64
	StartupRESTKlines     uint64
	DerivedKlines         uint64
	KlineCorrections      uint64
	KlineGapsDetected     uint64
	KlineGapBars          uint64
	KlineGapRequests      uint64
	KlineGapRequestErrors uint64
}

type collectorStats struct {
	processedEvents       atomic.Uint64
	droppedLatestEvents   atomic.Uint64
	coalescedLatestEvents atomic.Uint64
	flushedLatestEvents   atomic.Uint64
	processEventErrors    atomic.Uint64
	queuePeak             atomic.Int64
	lastEventReceivedAt   atomic.Int64
	lastEventProcessedAt  atomic.Int64
	sourceDelayMaxMillis  atomic.Int64
	queueDelayMaxMillis   atomic.Int64
	processMaxMillis      atomic.Int64
	outOfOrderEvents      atomic.Uint64
	duplicateKlineEvents  atomic.Uint64
	staleKlineEvents      atomic.Uint64
	openAfterClosedEvents atomic.Uint64
	webSocketKlineEvents  atomic.Uint64
	startupRESTKlines     atomic.Uint64
	derivedKlines         atomic.Uint64
	klineCorrections      atomic.Uint64
	klineGapsDetected     atomic.Uint64
	klineGapBars          atomic.Uint64
	klineGapRequests      atomic.Uint64
	klineGapRequestErrors atomic.Uint64
}

type Options struct {
	Symbols              []string
	Intervals            []string
	RESTLimit            int
	ReconnectDelay       time.Duration
	LiquidationLimit     int64
	PollOpenInterest     bool
	OpenInterestInterval time.Duration
	MarkPriceInterval    string
	WebSocketConnections int
	StartupLookback      int64
	BackfillIntervals    []string
	StartupDerivedRules  []aggregator.Rule
	GapPublisher         GapPublisher
	EventDrainTimeout    time.Duration
}

func New(
	options Options,
	rest exchange.RESTClient,
	ws exchange.WSClient,
	store Store,
) *Collector {
	eventQueueSize := adaptiveEventQueueSize(options)
	eventWorkers := adaptiveEventWorkers(options)
	return &Collector{
		options: options,
		rest:    rest,
		ws:      ws,
		store:   store,
		events: eventState{
			queue:             make(chan collectorEvent, eventQueueSize),
			workers:           eventWorkers,
			drainNotify:       make(chan struct{}, 1),
			latest:            make(map[string]collectorEvent),
			lastExchangeTimes: make(map[string]int64),
		},
		klines: klineState{
			versions:            make(map[string]map[int64]klineVersion),
			lastClosedOpenTimes: make(map[string]int64),
			pendingGaps:         make(map[string]map[string]klineGap),
		},
		now: time.Now,
	}
}

func DefaultReconnectDelay() time.Duration {
	return defaultReconnectDelay
}

func (c *Collector) Exchange() string {
	return c.rest.Exchange()
}

func (c *Collector) Market() string {
	return c.rest.Market()
}

func (c *Collector) Stats() Stats {
	return Stats{
		ProcessedEvents:       c.events.stats.processedEvents.Load(),
		DroppedLatestEvents:   c.events.stats.droppedLatestEvents.Load(),
		CoalescedLatestEvents: c.events.stats.coalescedLatestEvents.Load(),
		FlushedLatestEvents:   c.events.stats.flushedLatestEvents.Load(),
		ProcessEventErrors:    c.events.stats.processEventErrors.Load(),
		QueueLen:              len(c.events.queue),
		QueueCap:              cap(c.events.queue),
		QueuePeak:             c.events.stats.queuePeak.Load(),
		LastEventReceivedAt:   c.events.stats.lastEventReceivedAt.Load(),
		LastEventProcessedAt:  c.events.stats.lastEventProcessedAt.Load(),
		SourceDelayMaxMillis:  c.events.stats.sourceDelayMaxMillis.Load(),
		QueueDelayMaxMillis:   c.events.stats.queueDelayMaxMillis.Load(),
		ProcessMaxMillis:      c.events.stats.processMaxMillis.Load(),
		OutOfOrderEvents:      c.events.stats.outOfOrderEvents.Load(),
		DuplicateKlineEvents:  c.events.stats.duplicateKlineEvents.Load(),
		StaleKlineEvents:      c.events.stats.staleKlineEvents.Load(),
		OpenAfterClosedEvents: c.events.stats.openAfterClosedEvents.Load(),
		WebSocketKlineEvents:  c.events.stats.webSocketKlineEvents.Load(),
		StartupRESTKlines:     c.events.stats.startupRESTKlines.Load(),
		DerivedKlines:         c.events.stats.derivedKlines.Load(),
		KlineCorrections:      c.events.stats.klineCorrections.Load(),
		KlineGapsDetected:     c.events.stats.klineGapsDetected.Load(),
		KlineGapBars:          c.events.stats.klineGapBars.Load(),
		KlineGapRequests:      c.events.stats.klineGapRequests.Load(),
		KlineGapRequestErrors: c.events.stats.klineGapRequestErrors.Load(),
	}
}

func adaptiveEventQueueSize(options Options) int {
	streamCount := estimatedStreamCount(options)
	size := streamCount * 100
	if size < minEventQueueSize {
		return minEventQueueSize
	}
	if size > maxEventQueueSize {
		return maxEventQueueSize
	}
	return size
}

func adaptiveEventWorkers(options Options) int {
	streamCount := estimatedStreamCount(options)
	workers := defaultEventWorkers + streamCount/200
	if workers > maxEventWorkers {
		return maxEventWorkers
	}
	return workers
}

func estimatedStreamCount(options Options) int {
	return len(options.Symbols) * (len(options.Intervals) + 4)
}

func (c *Collector) Run(ctx context.Context) error {
	return c.run(ctx, true)
}

func (c *Collector) RunRealtime(ctx context.Context) error {
	return c.run(ctx, false)
}

func (c *Collector) run(ctx context.Context, backfill bool) error {
	producerCtx, cancelProducers := context.WithCancel(ctx)
	defer cancelProducers()
	if backfill {
		if err := c.Backfill(producerCtx); err != nil {
			return err
		}
	}

	workerCtx, cancelWorkers := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWorkers()
	var eventWorkerWG sync.WaitGroup
	c.startEventWorkers(workerCtx, &eventWorkerWG)
	c.startLatestEventFlusher(producerCtx, &eventWorkerWG)

	errCh := make(chan error, 2)

	go func() {
		errCh <- c.runWebSocketLoop(producerCtx)
	}()

	go func() {
		errCh <- c.runPollingLoop(producerCtx)
	}()

	firstErr := <-errCh
	if firstErr != nil && producerCtx.Err() == nil {
		c.setMarketUnavailable(producerCtx, firstErr.Error())
	}
	cancelProducers()
	secondErr := <-errCh
	drained := c.waitForEventDrain(c.eventDrainTimeout())
	if !drained {
		slog.Error(
			"collector event queue drain timed out",
			"exchange", c.rest.Exchange(),
			"market", c.rest.Market(),
			"timeout", c.eventDrainTimeout(),
			"pending_events", c.events.pending.Load(),
			"queue_length", len(c.events.queue),
			"queue_capacity", cap(c.events.queue),
		)
	}
	cancelWorkers()
	eventWorkerWG.Wait()
	if firstErr != nil {
		return firstErr
	}
	return secondErr
}

func (c *Collector) eventDrainTimeout() time.Duration {
	if c.options.EventDrainTimeout > 0 {
		return c.options.EventDrainTimeout
	}
	return defaultEventDrainTimeout
}

func (c *Collector) waitForEventDrain(timeout time.Duration) bool {
	if c.events.pending.Load() == 0 {
		return true
	}
	if timeout <= 0 {
		for c.events.pending.Load() > 0 {
			<-c.events.drainNotify
		}
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-c.events.drainNotify:
			if c.events.pending.Load() == 0 {
				return true
			}
		case <-timer.C:
			return c.events.pending.Load() == 0
		}
	}
}

func (c *Collector) addPendingEvent() {
	c.events.pending.Add(1)
}

func (c *Collector) completePendingEvent() {
	if c.events.pending.Add(-1) == 0 {
		select {
		case c.events.drainNotify <- struct{}{}:
		default:
		}
	}
}

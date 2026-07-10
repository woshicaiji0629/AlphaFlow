package collector

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"alphaflow/go-service/market-data/internal/aggregator"
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
	eventQueue          chan collectorEvent
	eventWorkers        int
	latestMu            sync.Mutex
	latestEvents        map[string]collectorEvent
	stats               collectorStats
	now                 func() time.Time
	lastBackfillRequest time.Time
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
}

type collectorStats struct {
	processedEvents       atomic.Uint64
	droppedLatestEvents   atomic.Uint64
	coalescedLatestEvents atomic.Uint64
	flushedLatestEvents   atomic.Uint64
	processEventErrors    atomic.Uint64
	queuePeak             atomic.Int64
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
		options:      options,
		rest:         rest,
		ws:           ws,
		store:        store,
		eventQueue:   make(chan collectorEvent, eventQueueSize),
		eventWorkers: eventWorkers,
		latestEvents: make(map[string]collectorEvent),
		now:          time.Now,
	}
}

func DefaultReconnectDelay() time.Duration {
	return defaultReconnectDelay
}

func (c *Collector) Stats() Stats {
	return Stats{
		ProcessedEvents:       c.stats.processedEvents.Load(),
		DroppedLatestEvents:   c.stats.droppedLatestEvents.Load(),
		CoalescedLatestEvents: c.stats.coalescedLatestEvents.Load(),
		FlushedLatestEvents:   c.stats.flushedLatestEvents.Load(),
		ProcessEventErrors:    c.stats.processEventErrors.Load(),
		QueueLen:              len(c.eventQueue),
		QueueCap:              cap(c.eventQueue),
		QueuePeak:             c.stats.queuePeak.Load(),
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var eventWorkerWG sync.WaitGroup
	c.startEventWorkers(ctx, &eventWorkerWG)
	c.startLatestEventFlusher(ctx, &eventWorkerWG)

	errCh := make(chan error, 3)

	go func() {
		errCh <- c.runWebSocketLoop(ctx)
	}()

	go func() {
		errCh <- c.runPollingLoop(ctx)
	}()

	if backfill {
		go func() {
			errCh <- c.runBackfillLoop(ctx)
		}()
	}

	err := <-errCh
	if err != nil && ctx.Err() == nil {
		c.setMarketUnavailable(ctx, err.Error())
	}
	cancel()
	eventWorkerWG.Wait()
	if err != nil {
		return err
	}

	err = <-errCh
	cancel()
	eventWorkerWG.Wait()
	return err
}

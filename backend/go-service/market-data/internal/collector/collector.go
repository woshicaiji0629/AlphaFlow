package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

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
	UpsertKline(ctx context.Context, kline model.Kline) error
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
	latestFlushInterval      = 100 * time.Millisecond
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
}

type collectorEventType string

const (
	collectorEventKline        collectorEventType = "kline"
	collectorEventLastPrice    collectorEventType = "last_price"
	collectorEventMarkPrice    collectorEventType = "mark_price"
	collectorEventBookTicker   collectorEventType = "book_ticker"
	collectorEventOpenInterest collectorEventType = "open_interest"
	collectorEventLiquidation  collectorEventType = "liquidation"
)

type collectorEvent struct {
	eventType    collectorEventType
	kline        model.Kline
	lastPrice    model.LastPrice
	markPrice    model.MarkPrice
	bookTicker   model.BookTicker
	openInterest model.OpenInterest
	liquidation  model.Liquidation
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

	go func() {
		errCh <- c.runBackfillLoop(ctx)
	}()

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

func (c *Collector) startEventWorkers(ctx context.Context, wg *sync.WaitGroup) {
	for worker := 0; worker < c.eventWorkers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			c.runEventWorker(ctx, worker)
		}(worker)
	}
}

func (c *Collector) startLatestEventFlusher(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.runLatestEventFlusher(ctx)
	}()
}

func (c *Collector) runLatestEventFlusher(ctx context.Context) {
	ticker := time.NewTicker(latestFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.flushLatestEvents(context.Background())
			return
		case <-ticker.C:
			c.flushLatestEvents(ctx)
		}
	}
}

func (c *Collector) runEventWorker(ctx context.Context, worker int) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-c.eventQueue:
			if err := c.processEvent(ctx, event); err != nil {
				c.stats.processEventErrors.Add(1)
				slog.Error(
					"process collector event failed",
					"exchange", c.rest.Exchange(),
					"market", c.rest.Market(),
					"worker", worker,
					"event_type", event.eventType,
					"symbol", event.symbol(),
					"interval", event.interval(),
					"error", err,
				)
			}
		}
	}
}

func (c *Collector) enqueueEvent(ctx context.Context, event collectorEvent) error {
	if event.isLatest() {
		c.coalesceLatestEvent(event)
		return nil
	}
	if event.isCritical() {
		select {
		case <-ctx.Done():
			return nil
		case c.eventQueue <- event:
			c.recordQueueLen()
			return nil
		}
	}

	timer := time.NewTimer(latestEventEnqueueTTL)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil
	case c.eventQueue <- event:
		c.recordQueueLen()
		return nil
	case <-timer.C:
		c.stats.droppedLatestEvents.Add(1)
		slog.Warn(
			"drop collector event after enqueue timeout",
			"exchange", c.rest.Exchange(),
			"market", c.rest.Market(),
			"event_type", event.eventType,
			"symbol", event.symbol(),
			"queue_length", len(c.eventQueue),
			"queue_capacity", cap(c.eventQueue),
		)
		return nil
	}
}

func (c *Collector) coalesceLatestEvent(event collectorEvent) {
	key := event.latestKey()
	c.latestMu.Lock()
	if _, ok := c.latestEvents[key]; ok {
		c.stats.coalescedLatestEvents.Add(1)
	}
	c.latestEvents[key] = event
	c.latestMu.Unlock()
}

func (c *Collector) flushLatestEvents(ctx context.Context) {
	events := c.drainLatestEvents()
	for _, event := range events {
		if err := c.processEvent(ctx, event); err != nil {
			c.stats.processEventErrors.Add(1)
			slog.Error(
				"flush latest collector event failed",
				"exchange", c.rest.Exchange(),
				"market", c.rest.Market(),
				"event_type", event.eventType,
				"symbol", event.symbol(),
				"error", err,
			)
			continue
		}
		c.stats.flushedLatestEvents.Add(1)
	}
}

func (c *Collector) drainLatestEvents() []collectorEvent {
	c.latestMu.Lock()
	defer c.latestMu.Unlock()
	if len(c.latestEvents) == 0 {
		return nil
	}

	events := make([]collectorEvent, 0, len(c.latestEvents))
	for key, event := range c.latestEvents {
		events = append(events, event)
		delete(c.latestEvents, key)
	}
	return events
}

func (c *Collector) processEvent(ctx context.Context, event collectorEvent) error {
	switch event.eventType {
	case collectorEventKline:
		if err := c.store.UpsertKline(ctx, event.kline); err != nil {
			return err
		}
	case collectorEventLastPrice:
		if err := c.store.SetLastPrice(ctx, event.lastPrice); err != nil {
			return err
		}
	case collectorEventMarkPrice:
		if err := c.store.SetMarkPrice(ctx, event.markPrice); err != nil {
			return err
		}
	case collectorEventBookTicker:
		if err := c.store.SetBookTicker(ctx, event.bookTicker); err != nil {
			return err
		}
	case collectorEventOpenInterest:
		if err := c.store.SetOpenInterest(ctx, event.openInterest); err != nil {
			return err
		}
	case collectorEventLiquidation:
		if err := c.store.AddLiquidation(ctx, event.liquidation, c.options.LiquidationLimit); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown collector event type %q", event.eventType)
	}
	if event.eventType != collectorEventOpenInterest {
		c.setMarketAvailable(ctx)
	}
	c.stats.processedEvents.Add(1)
	return nil
}

func (c *Collector) recordQueueLen() {
	queueLen := int64(len(c.eventQueue))
	for {
		peak := c.stats.queuePeak.Load()
		if queueLen <= peak || c.stats.queuePeak.CompareAndSwap(peak, queueLen) {
			return
		}
	}
}

func (event collectorEvent) isCritical() bool {
	return event.eventType == collectorEventKline || event.eventType == collectorEventLiquidation
}

func (event collectorEvent) isLatest() bool {
	switch event.eventType {
	case collectorEventLastPrice,
		collectorEventMarkPrice,
		collectorEventBookTicker,
		collectorEventOpenInterest:
		return true
	default:
		return false
	}
}

func (event collectorEvent) latestKey() string {
	return string(event.eventType) + ":" + event.symbol()
}

func (event collectorEvent) symbol() string {
	switch event.eventType {
	case collectorEventKline:
		return event.kline.Symbol
	case collectorEventLastPrice:
		return event.lastPrice.Symbol
	case collectorEventMarkPrice:
		return event.markPrice.Symbol
	case collectorEventBookTicker:
		return event.bookTicker.Symbol
	case collectorEventOpenInterest:
		return event.openInterest.Symbol
	case collectorEventLiquidation:
		return event.liquidation.Symbol
	default:
		return ""
	}
}

func (event collectorEvent) interval() string {
	if event.eventType == collectorEventKline {
		return event.kline.Interval
	}
	return ""
}

func (c *Collector) runWebSocketLoop(ctx context.Context) error {
	if c.ws == nil {
		return errors.New("nil websocket client")
	}
	streams := c.streams()
	shards := shardStreams(streams, defaultMaxStreams)
	if len(shards) == 0 {
		return errors.New("no websocket streams")
	}

	var wg sync.WaitGroup
	for index, shard := range shards {
		wg.Add(1)
		go func(shardIndex int, shardStreams []exchange.Stream) {
			defer wg.Done()
			c.runWebSocketShardLoop(ctx, shardIndex, len(shards), shardStreams)
		}(index, shard)
	}

	<-ctx.Done()
	wg.Wait()
	return nil
}

func (c *Collector) runBackfillLoop(ctx context.Context) error {
	if err := c.Backfill(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	<-ctx.Done()
	return nil
}

func (c *Collector) runWebSocketShardLoop(
	ctx context.Context,
	shardIndex int,
	shardCount int,
	streams []exchange.Stream,
) {
	baseReconnectDelay := c.options.ReconnectDelay
	var reconnectCount int64
	var consecutiveFailures int64
	shard := strconv.Itoa(shardIndex)

	for {
		startedAt := c.now()
		c.setWebSocketConnected(ctx, shard, reconnectCount, consecutiveFailures, startedAt, len(streams), shardCount)
		slog.Info("starting websocket", "shard", shard, "shards", shardCount, "streams", len(streams))
		err := c.ws.Run(ctx, streams, c)
		if ctx.Err() != nil {
			c.setWebSocketDisconnected(ctx, shard, nil, reconnectCount, consecutiveFailures, len(streams), shardCount)
			return
		}
		if c.now().Sub(startedAt) >= reconnectBackoffReset {
			consecutiveFailures = 0
		}
		consecutiveFailures++
		reconnectCount++
		reconnectDelay := nextReconnectDelay(baseReconnectDelay, consecutiveFailures)
		c.setWebSocketDisconnected(ctx, shard, err, reconnectCount, consecutiveFailures, len(streams), shardCount)
		slog.Warn(
			"websocket stopped",
			"shard", shard,
			"shards", shardCount,
			"error", err,
			"reconnect_delay", reconnectDelay,
			"reconnect_count", reconnectCount,
			"consecutive_failures", consecutiveFailures,
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func shardStreams(streams []exchange.Stream, maxStreams int) [][]exchange.Stream {
	if len(streams) == 0 {
		return nil
	}
	if maxStreams <= 0 {
		maxStreams = defaultMaxStreams
	}

	shards := make([][]exchange.Stream, 0, (len(streams)+maxStreams-1)/maxStreams)
	for start := 0; start < len(streams); start += maxStreams {
		end := start + maxStreams
		if end > len(streams) {
			end = len(streams)
		}
		shards = append(shards, streams[start:end])
	}
	return shards
}

func nextReconnectDelay(base time.Duration, consecutiveFailures int64) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if consecutiveFailures <= 1 {
		return clampReconnectDelay(base)
	}

	delay := base
	for i := int64(1); i < consecutiveFailures; i++ {
		if delay >= maxReconnectDelay/2 {
			return maxReconnectDelay
		}
		delay *= 2
	}
	return clampReconnectDelay(delay)
}

func clampReconnectDelay(delay time.Duration) time.Duration {
	if delay > maxReconnectDelay {
		return maxReconnectDelay
	}
	return delay
}

func (c *Collector) runPollingLoop(ctx context.Context) error {
	if !c.options.PollOpenInterest {
		<-ctx.Done()
		return nil
	}

	interval := c.options.OpenInterestInterval

	if err := c.waitOpenInterestStartup(ctx); err != nil {
		return nil
	}
	c.pollOpenInterest(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.pollOpenInterest(ctx)
		}
	}
}

func (c *Collector) waitOpenInterestStartup(ctx context.Context) error {
	timer := time.NewTimer(openInterestStartupDelay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Collector) pollOpenInterest(ctx context.Context) {
	for _, symbol := range c.openInterestSymbols() {
		if ctx.Err() != nil {
			return
		}
		interest, err := c.rest.FetchOpenInterest(ctx, symbol)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("fetch open interest failed", "symbol", symbol, "error", err)
			continue
		}
		if err := c.store.SetOpenInterest(ctx, interest); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("store open interest failed", "symbol", symbol, "error", err)
			continue
		}
		slog.Debug("updated open interest", "symbol", symbol)
	}
}

func (c *Collector) openInterestSymbols() []string {
	if len(c.options.Symbols) <= maxOpenInterestSymbols {
		return c.options.Symbols
	}
	return c.options.Symbols[:maxOpenInterestSymbols]
}

func (c *Collector) Backfill(ctx context.Context) error {
	var attempts int
	var successes int
	var failures int
	for _, interval := range c.backfillIntervals() {
		for _, symbol := range c.options.Symbols {
			startTime, err := c.nextStartTime(ctx, symbol, interval)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				failures++
				slog.Warn("prepare backfill failed", "symbol", symbol, "interval", interval, "error", err)
				continue
			}
			if !c.hasClosedWindowToBackfill(interval, startTime) {
				slog.Debug("skip backfill without closed window", "symbol", symbol, "interval", interval, "start_time", startTime)
				continue
			}

			attempts++
			if err := c.waitBackfillRequest(ctx); err != nil {
				return err
			}
			klines, err := c.rest.FetchKlines(
				ctx,
				symbol,
				interval,
				c.options.RESTLimit,
				startTime,
			)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				failures++
				slog.Warn("backfill klines failed", "symbol", symbol, "interval", interval, "error", err)
				continue
			}
			stored := 0
			for _, kline := range klines {
				if err := c.store.UpsertKline(ctx, kline); err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					failures++
					slog.Warn("store backfill kline failed", "symbol", symbol, "interval", interval, "open_time", kline.OpenTime, "error", err)
					continue
				}
				stored++
			}
			if stored > 0 {
				successes++
				c.setMarketAvailable(ctx)
			}
			slog.Info("backfilled klines", "symbol", symbol, "interval", interval, "count", len(klines))
		}
	}
	if attempts > 0 && successes == 0 && failures > 0 {
		return fmt.Errorf("all backfill attempts failed: attempts=%d failures=%d", attempts, failures)
	}
	return nil
}

func (c *Collector) backfillIntervals() []string {
	seen := make(map[string]struct{}, len(c.options.Intervals))
	for _, interval := range c.options.Intervals {
		seen[interval] = struct{}{}
	}

	result := make([]string, 0, len(c.options.Intervals))
	for _, interval := range backfillIntervalPriority {
		if _, ok := seen[interval]; !ok {
			continue
		}
		result = append(result, interval)
		delete(seen, interval)
	}
	for _, interval := range c.options.Intervals {
		if _, ok := seen[interval]; !ok {
			continue
		}
		result = append(result, interval)
		delete(seen, interval)
	}
	return result
}

func (c *Collector) waitBackfillRequest(ctx context.Context) error {
	if c.lastBackfillRequest.IsZero() {
		c.lastBackfillRequest = c.now()
		return nil
	}
	nextRequestAt := c.lastBackfillRequest.Add(backfillRequestInterval)
	wait := nextRequestAt.Sub(c.now())
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	c.lastBackfillRequest = c.now()
	return nil
}

func (c *Collector) hasClosedWindowToBackfill(interval string, startTime int64) bool {
	if startTime <= 0 {
		return true
	}
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return true
	}
	return startTime <= c.now().UnixMilli()-intervalMillis
}

func (c *Collector) nextStartTime(ctx context.Context, symbol string, interval string) (int64, error) {
	lastOpenTime, ok, err := c.store.LastOpenTime(
		ctx,
		c.rest.Exchange(),
		c.rest.Market(),
		symbol,
		interval,
	)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return 0, err
	}
	return lastOpenTime + intervalMillis, nil
}

func (c *Collector) HandleKline(ctx context.Context, kline model.Kline) error {
	if kline.Exchange == "" || kline.Market == "" || kline.Symbol == "" || kline.Interval == "" {
		return errors.New("invalid empty kline identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType: collectorEventKline,
		kline:     kline,
	})
}

func (c *Collector) HandleLastPrice(ctx context.Context, price model.LastPrice) error {
	if price.Exchange == "" || price.Market == "" || price.Symbol == "" {
		return errors.New("invalid empty last price identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType: collectorEventLastPrice,
		lastPrice: price,
	})
}

func (c *Collector) HandleMarkPrice(ctx context.Context, price model.MarkPrice) error {
	if price.Exchange == "" || price.Market == "" || price.Symbol == "" {
		return errors.New("invalid empty mark price identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType: collectorEventMarkPrice,
		markPrice: price,
	})
}

func (c *Collector) HandleBookTicker(ctx context.Context, ticker model.BookTicker) error {
	if ticker.Exchange == "" || ticker.Market == "" || ticker.Symbol == "" {
		return errors.New("invalid empty book ticker identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType:  collectorEventBookTicker,
		bookTicker: ticker,
	})
}

func (c *Collector) HandleOpenInterest(ctx context.Context, interest model.OpenInterest) error {
	if interest.Exchange == "" || interest.Market == "" || interest.Symbol == "" {
		return errors.New("invalid empty open interest identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType:    collectorEventOpenInterest,
		openInterest: interest,
	})
}

func (c *Collector) HandleLiquidation(ctx context.Context, liquidation model.Liquidation) error {
	if liquidation.Exchange == "" || liquidation.Market == "" || liquidation.Symbol == "" {
		return errors.New("invalid empty liquidation identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType:   collectorEventLiquidation,
		liquidation: liquidation,
	})
}

func (c *Collector) setMarketAvailable(ctx context.Context) {
	c.setMarketStatus(ctx, true, "")
}

func (c *Collector) setMarketUnavailable(ctx context.Context, reason string) {
	c.setMarketStatus(ctx, false, reason)
}

func (c *Collector) setMarketStatus(ctx context.Context, available bool, reason string) {
	if ctx.Err() != nil {
		return
	}
	if err := c.store.SetMarketStatus(ctx, model.MarketStatus{
		Exchange:  c.rest.Exchange(),
		Market:    c.rest.Market(),
		Available: available,
		Reason:    reason,
		UpdatedAt: time.Now().UnixMilli(),
	}); err != nil {
		slog.Error(
			"set market status failed",
			"exchange", c.rest.Exchange(),
			"market", c.rest.Market(),
			"available", available,
			"error", err,
		)
	}
}

func (c *Collector) setWebSocketConnected(
	ctx context.Context,
	shard string,
	reconnectCount int64,
	consecutiveFailures int64,
	startedAt time.Time,
	streamCount int,
	connectionCount int,
) {
	c.setWebSocketStatus(ctx, model.WebSocketStatus{
		Exchange:            c.rest.Exchange(),
		Market:              c.rest.Market(),
		Shard:               shard,
		Connected:           true,
		LastStartedAt:       startedAt.UnixMilli(),
		StreamCount:         streamCount,
		ConnectionCount:     connectionCount,
		ReconnectCount:      reconnectCount,
		ConsecutiveFailures: consecutiveFailures,
		UpdatedAt:           c.now().UnixMilli(),
	})
}

func (c *Collector) setWebSocketDisconnected(
	ctx context.Context,
	shard string,
	err error,
	reconnectCount int64,
	consecutiveFailures int64,
	streamCount int,
	connectionCount int,
) {
	status := model.WebSocketStatus{
		Exchange:            c.rest.Exchange(),
		Market:              c.rest.Market(),
		Shard:               shard,
		Connected:           false,
		LastStoppedAt:       c.now().UnixMilli(),
		StreamCount:         streamCount,
		ConnectionCount:     connectionCount,
		ReconnectCount:      reconnectCount,
		ConsecutiveFailures: consecutiveFailures,
		UpdatedAt:           c.now().UnixMilli(),
	}
	if err != nil {
		status.LastError = err.Error()
	}
	c.setWebSocketStatus(ctx, status)
}

func (c *Collector) setWebSocketStatus(ctx context.Context, status model.WebSocketStatus) {
	if ctx.Err() != nil {
		return
	}
	if err := c.store.SetWebSocketStatus(ctx, status); err != nil {
		slog.Error(
			"set websocket status failed",
			"exchange", status.Exchange,
			"market", status.Market,
			"connected", status.Connected,
			"error", err,
		)
	}
}

func (c *Collector) streams() []exchange.Stream {
	streams := make(
		[]exchange.Stream,
		0,
		len(c.options.Symbols)*(len(c.options.Intervals)+4),
	)
	for _, symbol := range c.options.Symbols {
		for _, interval := range c.options.Intervals {
			streams = append(streams, exchange.Stream{
				Symbol:   symbol,
				Interval: interval,
				Type:     exchange.StreamTypeKline,
			})
		}
		streams = append(streams, exchange.Stream{
			Symbol: symbol,
			Type:   exchange.StreamTypeAggTrade,
		})
		streams = append(streams, exchange.Stream{
			Symbol:   symbol,
			Interval: c.options.MarkPriceInterval,
			Type:     exchange.StreamTypeMarkPrice,
		})
		streams = append(streams, exchange.Stream{
			Symbol: symbol,
			Type:   exchange.StreamTypeBookTicker,
		})
		streams = append(streams, exchange.Stream{
			Symbol: symbol,
			Type:   exchange.StreamTypeForceOrder,
		})
	}
	return streams
}

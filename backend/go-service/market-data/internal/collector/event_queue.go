package collector

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"alphaflow/go-service/market-data/internal/backfillqueue"
	"alphaflow/go-service/market-data/internal/model"
)

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
	klineSource  klineSource
	lastPrice    model.LastPrice
	markPrice    model.MarkPrice
	bookTicker   model.BookTicker
	openInterest model.OpenInterest
	liquidation  model.Liquidation
	timing       eventTiming
}

type eventTiming struct {
	exchangeTime int64
	receivedAt   int64
	enqueuedAt   int64
}

type klineVersion struct {
	closed    bool
	eventTime int64
	source    klineSource
}

type klineSource string

const (
	klineSourceWebSocket   klineSource = "websocket"
	klineSourceStartupREST klineSource = "startup_rest"
	klineSourceDerived     klineSource = "derived"
)

type klineReservation struct {
	streamKey  string
	openTime   int64
	previous   klineVersion
	existed    bool
	current    klineVersion
	correction bool
}

type klineGap struct {
	kline model.Kline
	start int64
	end   int64
}

type klineDecision string

const (
	klineAccept           klineDecision = "accept"
	klineDuplicate        klineDecision = "duplicate"
	klineStale            klineDecision = "stale"
	klineOpenAfterClosed  klineDecision = "open_after_closed"
	klineVersionRetention               = 8
)

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
			func() {
				defer c.completePendingEvent()
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
			}()
		}
	}
}

func (c *Collector) enqueueEvent(ctx context.Context, event collectorEvent) error {
	c.prepareEventTiming(&event)
	if event.isLatest() {
		c.coalesceLatestEvent(event)
		return nil
	}
	if event.isCritical() {
		c.addPendingEvent()
		select {
		case <-ctx.Done():
			c.completePendingEvent()
			return nil
		case c.eventQueue <- event:
			c.recordQueueLen()
			return nil
		}
	}

	timer := time.NewTimer(latestEventEnqueueTTL)
	defer timer.Stop()
	c.addPendingEvent()
	select {
	case <-ctx.Done():
		c.completePendingEvent()
		return nil
	case c.eventQueue <- event:
		c.recordQueueLen()
		return nil
	case <-timer.C:
		c.completePendingEvent()
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
	startedAt := c.now().UnixMilli()
	if event.timing.enqueuedAt > 0 {
		recordAtomicMax(&c.stats.queueDelayMaxMillis, startedAt-event.timing.enqueuedAt)
	}
	defer func() {
		finishedAt := c.now().UnixMilli()
		recordAtomicMax(&c.stats.processMaxMillis, finishedAt-startedAt)
		c.stats.lastEventProcessedAt.Store(finishedAt)
	}()
	switch event.eventType {
	case collectorEventKline:
		writeLock := c.klineWriteLock(event.kline)
		writeLock.Lock()
		defer writeLock.Unlock()
		reservation, decision := c.reserveKline(event.kline, event.klineSource)
		if decision != klineAccept {
			c.recordKlineDecision(decision)
			return nil
		}
		if err := c.store.UpsertKline(ctx, event.kline); err != nil {
			c.rollbackKline(reservation)
			return err
		}
		c.commitKline(reservation)
		c.recordAcceptedKline(reservation)
		c.recordKlineContinuity(ctx, event.kline)
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

func (c *Collector) reserveKline(kline model.Kline, source klineSource) (klineReservation, klineDecision) {
	streamKey := klineStreamKey(kline)
	if source == "" {
		source = klineSourceWebSocket
	}
	current := klineVersion{closed: kline.IsClosed, eventTime: kline.EventTime, source: source}
	c.klineVersionMu.Lock()
	defer c.klineVersionMu.Unlock()
	versions := c.klineVersions[streamKey]
	if versions == nil {
		versions = make(map[int64]klineVersion)
		c.klineVersions[streamKey] = versions
	}
	previous, existed := versions[kline.OpenTime]
	decision := compareKlineVersion(previous, existed, current)
	reservation := klineReservation{streamKey: streamKey, openTime: kline.OpenTime, previous: previous, existed: existed, current: current, correction: existed && previous.closed && current.closed}
	if decision != klineAccept {
		return reservation, decision
	}
	versions[kline.OpenTime] = current
	return reservation, klineAccept
}

func (c *Collector) commitKline(reservation klineReservation) {
	c.klineVersionMu.Lock()
	defer c.klineVersionMu.Unlock()
	pruneKlineVersions(c.klineVersions[reservation.streamKey], klineVersionRetention)
}

func (c *Collector) recordAcceptedKline(reservation klineReservation) {
	switch reservation.current.source {
	case klineSourceWebSocket:
		c.stats.webSocketKlineEvents.Add(1)
	case klineSourceStartupREST:
		c.stats.startupRESTKlines.Add(1)
	case klineSourceDerived:
		c.stats.derivedKlines.Add(1)
	}
	if reservation.correction {
		c.stats.klineCorrections.Add(1)
	}
}

func (c *Collector) rememberStoredKlines(klines []model.Kline, source klineSource, countWrite bool) {
	for _, kline := range klines {
		current := klineVersion{closed: kline.IsClosed, eventTime: kline.EventTime, source: source}
		streamKey := klineStreamKey(kline)
		c.klineVersionMu.Lock()
		versions := c.klineVersions[streamKey]
		if versions == nil {
			versions = make(map[int64]klineVersion)
			c.klineVersions[streamKey] = versions
		}
		previous, existed := versions[kline.OpenTime]
		if compareKlineVersion(previous, existed, current) == klineAccept {
			versions[kline.OpenTime] = current
			pruneKlineVersions(versions, klineVersionRetention)
		}
		if kline.IsClosed && kline.OpenTime > c.lastClosedOpenTimes[streamKey] {
			c.lastClosedOpenTimes[streamKey] = kline.OpenTime
		}
		c.klineVersionMu.Unlock()
	}
	if !countWrite {
		return
	}
	switch source {
	case klineSourceStartupREST:
		c.stats.startupRESTKlines.Add(uint64(len(klines)))
	case klineSourceDerived:
		c.stats.derivedKlines.Add(uint64(len(klines)))
	}
}

func (c *Collector) recordKlineContinuity(ctx context.Context, kline model.Kline) {
	if !kline.IsClosed {
		return
	}
	intervalMillis, err := model.IntervalMillis(kline.Interval)
	if err != nil || intervalMillis <= 0 {
		return
	}
	streamKey := klineStreamKey(kline)
	c.klineContinuityMu.Lock()
	defer c.klineContinuityMu.Unlock()
	last := c.lastClosedOpenTimes[streamKey]
	if kline.OpenTime > last {
		c.lastClosedOpenTimes[streamKey] = kline.OpenTime
	}
	if last > 0 && kline.OpenTime > last+intervalMillis {
		missingBars := uint64((kline.OpenTime-last)/intervalMillis - 1)
		if missingBars > 0 {
			c.stats.klineGapsDetected.Add(1)
			c.stats.klineGapBars.Add(missingBars)
			gap := klineGap{kline: kline, start: last + intervalMillis, end: kline.OpenTime}
			if c.options.GapPublisher != nil {
				if c.pendingKlineGaps[streamKey] == nil {
					c.pendingKlineGaps[streamKey] = make(map[string]klineGap)
				}
				c.pendingKlineGaps[streamKey][gapKey(gap.start, gap.end)] = gap
			}
			slog.Warn("closed kline gap detected", "exchange", kline.Exchange, "market", kline.Market, "symbol", kline.Symbol, "interval", kline.Interval, "last_open_time", last, "current_open_time", kline.OpenTime, "gap_start", gap.start, "gap_end_exclusive", gap.end, "missing_bars", missingBars)
		}
	}
	for key, gap := range c.pendingKlineGaps[streamKey] {
		if c.publishGapRepair(ctx, gap.kline, gap.start, gap.end) {
			delete(c.pendingKlineGaps[streamKey], key)
		}
	}
}

func (c *Collector) publishGapRepair(ctx context.Context, kline model.Kline, start int64, end int64) bool {
	if c.options.GapPublisher == nil {
		return false
	}
	task := backfillqueue.DefaultTask()
	task.Exchange = kline.Exchange
	task.Symbol = kline.Symbol
	task.Intervals = []string{kline.Interval}
	task.Start = time.UnixMilli(start).UTC().Format("200601021504")
	task.End = time.UnixMilli(end).UTC().Format("200601021504")
	task.Source = "collector_gap"
	task.Reason = "closed_kline_gap"
	messageID, err := c.options.GapPublisher.Publish(ctx, task)
	if err != nil {
		c.stats.klineGapRequestErrors.Add(1)
		slog.Error("publish kline gap repair failed", "exchange", kline.Exchange, "market", kline.Market, "symbol", kline.Symbol, "interval", kline.Interval, "start", start, "end", end, "error", err)
		return false
	}
	c.stats.klineGapRequests.Add(1)
	slog.Info("published kline gap repair", "message_id", messageID, "exchange", kline.Exchange, "market", kline.Market, "symbol", kline.Symbol, "interval", kline.Interval, "start", start, "end", end)
	return true
}

func (c *Collector) klineWriteLock(kline model.Kline) *sync.Mutex {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(klineStreamKey(kline)))
	_, _ = hasher.Write([]byte(fmt.Sprintf(":%d", kline.OpenTime)))
	return &c.klineWriteLocks[hasher.Sum32()%uint32(len(c.klineWriteLocks))]
}

func gapKey(start, end int64) string { return fmt.Sprintf("%d:%d", start, end) }

func compareKlineVersion(previous klineVersion, existed bool, current klineVersion) klineDecision {
	if !existed {
		return klineAccept
	}
	if previous.closed && !current.closed {
		return klineOpenAfterClosed
	}
	if !previous.closed && current.closed {
		return klineAccept
	}
	if current.eventTime == 0 {
		if current.closed {
			return klineDuplicate
		}
		return klineAccept
	}
	if previous.eventTime > current.eventTime {
		return klineStale
	}
	if previous.eventTime == current.eventTime {
		return klineDuplicate
	}
	return klineAccept
}

func (c *Collector) rollbackKline(reservation klineReservation) {
	c.klineVersionMu.Lock()
	defer c.klineVersionMu.Unlock()
	versions := c.klineVersions[reservation.streamKey]
	if versions == nil || versions[reservation.openTime] != reservation.current {
		return
	}
	if reservation.existed {
		versions[reservation.openTime] = reservation.previous
		return
	}
	delete(versions, reservation.openTime)
}

func (c *Collector) recordKlineDecision(decision klineDecision) {
	switch decision {
	case klineDuplicate:
		c.stats.duplicateKlineEvents.Add(1)
	case klineStale:
		c.stats.staleKlineEvents.Add(1)
	case klineOpenAfterClosed:
		c.stats.openAfterClosedEvents.Add(1)
	}
}

func klineStreamKey(kline model.Kline) string {
	return kline.Exchange + ":" + kline.Market + ":" + kline.Symbol + ":" + kline.Interval
}

func pruneKlineVersions(versions map[int64]klineVersion, limit int) {
	for len(versions) > limit {
		var oldest int64
		first := true
		for openTime := range versions {
			if first || openTime < oldest {
				oldest = openTime
				first = false
			}
		}
		delete(versions, oldest)
	}
}

func (c *Collector) prepareEventTiming(event *collectorEvent) {
	now := c.now().UnixMilli()
	event.timing.exchangeTime = event.exchangeTime()
	event.timing.receivedAt = now
	event.timing.enqueuedAt = now
	c.stats.lastEventReceivedAt.Store(now)
	if event.timing.exchangeTime <= 0 {
		return
	}
	recordAtomicMax(&c.stats.sourceDelayMaxMillis, now-event.timing.exchangeTime)
	c.recordEventOrder(*event)
}

func (c *Collector) recordEventOrder(event collectorEvent) {
	key := event.orderKey()
	if key == "" {
		return
	}
	c.eventTimingMu.Lock()
	last := c.lastExchangeTimes[key]
	if event.timing.exchangeTime >= last {
		c.lastExchangeTimes[key] = event.timing.exchangeTime
	}
	c.eventTimingMu.Unlock()
	if last > 0 && event.timing.exchangeTime < last {
		c.stats.outOfOrderEvents.Add(1)
	}
}

func recordAtomicMax(target *atomic.Int64, value int64) {
	if value < 0 {
		return
	}
	for {
		current := target.Load()
		if value <= current || target.CompareAndSwap(current, value) {
			return
		}
	}
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
	return (event.eventType == collectorEventKline && event.kline.IsClosed) ||
		event.eventType == collectorEventLiquidation
}

func (event collectorEvent) isLatest() bool {
	if event.eventType == collectorEventKline {
		return !event.kline.IsClosed
	}
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
	if event.eventType == collectorEventKline {
		return string(event.eventType) + ":" + event.kline.Symbol + ":" + event.kline.Interval
	}
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

func (event collectorEvent) exchangeTime() int64 {
	switch event.eventType {
	case collectorEventKline:
		return event.kline.EventTime
	case collectorEventLastPrice:
		return event.lastPrice.EventTime
	case collectorEventMarkPrice:
		return event.markPrice.EventTime
	case collectorEventBookTicker:
		return event.bookTicker.EventTime
	case collectorEventOpenInterest:
		return event.openInterest.Time
	case collectorEventLiquidation:
		return event.liquidation.EventTime
	default:
		return 0
	}
}

func (event collectorEvent) orderKey() string {
	if event.symbol() == "" || event.timing.exchangeTime <= 0 {
		return ""
	}
	return string(event.eventType) + ":" + event.symbol() + ":" + event.interval()
}

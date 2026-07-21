package collector

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"

	"alphaflow/go-service/market-data/internal/backfillqueue"
	"alphaflow/go-service/market-data/internal/model"
)

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

func (c *Collector) reserveKline(kline model.Kline, source klineSource) (klineReservation, klineDecision) {
	streamKey := klineStreamKey(kline)
	if source == "" {
		source = klineSourceWebSocket
	}
	current := klineVersion{closed: kline.IsClosed, eventTime: kline.EventTime, source: source}
	c.klines.versionMu.Lock()
	defer c.klines.versionMu.Unlock()
	versions := c.klines.versions[streamKey]
	if versions == nil {
		versions = make(map[int64]klineVersion)
		c.klines.versions[streamKey] = versions
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
	c.klines.versionMu.Lock()
	defer c.klines.versionMu.Unlock()
	pruneKlineVersions(c.klines.versions[reservation.streamKey], klineVersionRetention)
}

func (c *Collector) recordAcceptedKline(reservation klineReservation) {
	switch reservation.current.source {
	case klineSourceWebSocket:
		c.events.stats.webSocketKlineEvents.Add(1)
	case klineSourceStartupREST:
		c.events.stats.startupRESTKlines.Add(1)
	case klineSourceDerived:
		c.events.stats.derivedKlines.Add(1)
	}
	if reservation.correction {
		c.events.stats.klineCorrections.Add(1)
	}
}

func (c *Collector) rememberStoredKlines(klines []model.Kline, source klineSource, countWrite bool) {
	for _, kline := range klines {
		current := klineVersion{closed: kline.IsClosed, eventTime: kline.EventTime, source: source}
		streamKey := klineStreamKey(kline)
		c.klines.versionMu.Lock()
		versions := c.klines.versions[streamKey]
		if versions == nil {
			versions = make(map[int64]klineVersion)
			c.klines.versions[streamKey] = versions
		}
		previous, existed := versions[kline.OpenTime]
		if compareKlineVersion(previous, existed, current) == klineAccept {
			versions[kline.OpenTime] = current
			pruneKlineVersions(versions, klineVersionRetention)
		}
		c.klines.versionMu.Unlock()
		if kline.IsClosed {
			c.rememberClosedOpenTime(streamKey, kline.OpenTime)
		}
	}
	if !countWrite {
		return
	}
	switch source {
	case klineSourceStartupREST:
		c.events.stats.startupRESTKlines.Add(uint64(len(klines)))
	case klineSourceDerived:
		c.events.stats.derivedKlines.Add(uint64(len(klines)))
	}
}

func (c *Collector) rememberClosedOpenTime(streamKey string, openTime int64) {
	c.klines.continuityMu.Lock()
	defer c.klines.continuityMu.Unlock()
	if openTime > c.klines.lastClosedOpenTimes[streamKey] {
		c.klines.lastClosedOpenTimes[streamKey] = openTime
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
	c.klines.continuityMu.Lock()
	defer c.klines.continuityMu.Unlock()
	last := c.klines.lastClosedOpenTimes[streamKey]
	if kline.OpenTime > last {
		c.klines.lastClosedOpenTimes[streamKey] = kline.OpenTime
	}
	if last > 0 && kline.OpenTime > last+intervalMillis {
		missingBars := uint64((kline.OpenTime-last)/intervalMillis - 1)
		if missingBars > 0 {
			c.events.stats.klineGapsDetected.Add(1)
			c.events.stats.klineGapBars.Add(missingBars)
			gap := klineGap{kline: kline, start: last + intervalMillis, end: kline.OpenTime}
			if c.options.GapPublisher != nil {
				if c.klines.pendingGaps[streamKey] == nil {
					c.klines.pendingGaps[streamKey] = make(map[string]klineGap)
				}
				c.klines.pendingGaps[streamKey][gapKey(gap.start, gap.end)] = gap
			}
			slog.Warn("closed kline gap detected", "exchange", kline.Exchange, "market", kline.Market, "symbol", kline.Symbol, "interval", kline.Interval, "last_open_time", last, "current_open_time", kline.OpenTime, "gap_start", gap.start, "gap_end_exclusive", gap.end, "missing_bars", missingBars)
		}
	}
	for key, gap := range c.klines.pendingGaps[streamKey] {
		if c.publishGapRepair(ctx, gap.kline, gap.start, gap.end) {
			delete(c.klines.pendingGaps[streamKey], key)
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
		c.events.stats.klineGapRequestErrors.Add(1)
		slog.Error("publish kline gap repair failed", "exchange", kline.Exchange, "market", kline.Market, "symbol", kline.Symbol, "interval", kline.Interval, "start", start, "end", end, "error", err)
		return false
	}
	c.events.stats.klineGapRequests.Add(1)
	slog.Info("published kline gap repair", "message_id", messageID, "exchange", kline.Exchange, "market", kline.Market, "symbol", kline.Symbol, "interval", kline.Interval, "start", start, "end", end)
	return true
}

func (c *Collector) klineWriteLock(kline model.Kline) *sync.Mutex {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(klineStreamKey(kline)))
	_, _ = hasher.Write([]byte(fmt.Sprintf(":%d", kline.OpenTime)))
	return &c.klines.writeLocks[hasher.Sum32()%uint32(len(c.klines.writeLocks))]
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
	c.klines.versionMu.Lock()
	defer c.klines.versionMu.Unlock()
	versions := c.klines.versions[reservation.streamKey]
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
		c.events.stats.duplicateKlineEvents.Add(1)
	case klineStale:
		c.events.stats.staleKlineEvents.Add(1)
	case klineOpenAfterClosed:
		c.events.stats.openAfterClosedEvents.Add(1)
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

package collector

import (
	"sync/atomic"

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

func (c *Collector) prepareEventTiming(event *collectorEvent) {
	now := c.now().UnixMilli()
	event.timing.exchangeTime = event.exchangeTime()
	event.timing.receivedAt = now
	event.timing.enqueuedAt = now
	c.events.stats.lastEventReceivedAt.Store(now)
	if event.timing.exchangeTime <= 0 {
		return
	}
	recordAtomicMax(&c.events.stats.sourceDelayMaxMillis, now-event.timing.exchangeTime)
	c.recordEventOrder(*event)
}

func (c *Collector) recordEventOrder(event collectorEvent) {
	key := event.orderKey()
	if key == "" {
		return
	}
	c.events.timingMu.Lock()
	last := c.events.lastExchangeTimes[key]
	if event.timing.exchangeTime >= last {
		c.events.lastExchangeTimes[key] = event.timing.exchangeTime
	}
	c.events.timingMu.Unlock()
	if last > 0 && event.timing.exchangeTime < last {
		c.events.stats.outOfOrderEvents.Add(1)
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
	queueLen := int64(len(c.events.queue))
	for {
		peak := c.events.stats.queuePeak.Load()
		if queueLen <= peak || c.events.stats.queuePeak.CompareAndSwap(peak, queueLen) {
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

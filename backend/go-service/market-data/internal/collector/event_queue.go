package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

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
	lastPrice    model.LastPrice
	markPrice    model.MarkPrice
	bookTicker   model.BookTicker
	openInterest model.OpenInterest
	liquidation  model.Liquidation
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

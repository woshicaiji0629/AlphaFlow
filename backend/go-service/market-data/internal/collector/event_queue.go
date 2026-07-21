package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

func (c *Collector) startEventWorkers(ctx context.Context, wg *sync.WaitGroup) {
	for worker := 0; worker < c.events.workers; worker++ {
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
			flushCtx, cancel := context.WithTimeout(context.Background(), c.eventDrainTimeout())
			c.flushLatestEvents(flushCtx)
			if flushCtx.Err() != nil {
				slog.Warn("collector latest event flush timed out", "exchange", c.rest.Exchange(), "market", c.rest.Market(), "timeout", c.eventDrainTimeout())
			}
			cancel()
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
		case event := <-c.events.queue:
			func() {
				defer c.completePendingEvent()
				if err := c.processEvent(ctx, event); err != nil {
					c.events.stats.processEventErrors.Add(1)
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
		case c.events.queue <- event:
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
	case c.events.queue <- event:
		c.recordQueueLen()
		return nil
	case <-timer.C:
		c.completePendingEvent()
		c.events.stats.droppedLatestEvents.Add(1)
		slog.Warn(
			"drop collector event after enqueue timeout",
			"exchange", c.rest.Exchange(),
			"market", c.rest.Market(),
			"event_type", event.eventType,
			"symbol", event.symbol(),
			"queue_length", len(c.events.queue),
			"queue_capacity", cap(c.events.queue),
		)
		return nil
	}
}

func (c *Collector) coalesceLatestEvent(event collectorEvent) {
	key := event.latestKey()
	c.events.latestMu.Lock()
	if _, ok := c.events.latest[key]; ok {
		c.events.stats.coalescedLatestEvents.Add(1)
	}
	c.events.latest[key] = event
	c.events.latestMu.Unlock()
}

func (c *Collector) flushLatestEvents(ctx context.Context) {
	events := c.drainLatestEvents()
	for _, event := range events {
		if err := c.processEvent(ctx, event); err != nil {
			c.events.stats.processEventErrors.Add(1)
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
		c.events.stats.flushedLatestEvents.Add(1)
	}
}

func (c *Collector) drainLatestEvents() []collectorEvent {
	c.events.latestMu.Lock()
	defer c.events.latestMu.Unlock()
	if len(c.events.latest) == 0 {
		return nil
	}

	events := make([]collectorEvent, 0, len(c.events.latest))
	for key, event := range c.events.latest {
		events = append(events, event)
		delete(c.events.latest, key)
	}
	return events
}

func (c *Collector) processEvent(ctx context.Context, event collectorEvent) error {
	startedAt := c.now().UnixMilli()
	if event.timing.enqueuedAt > 0 {
		recordAtomicMax(&c.events.stats.queueDelayMaxMillis, startedAt-event.timing.enqueuedAt)
	}
	defer func() {
		finishedAt := c.now().UnixMilli()
		recordAtomicMax(&c.events.stats.processMaxMillis, finishedAt-startedAt)
		c.events.stats.lastEventProcessedAt.Store(finishedAt)
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
	c.events.stats.processedEvents.Add(1)
	return nil
}

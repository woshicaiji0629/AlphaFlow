package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

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
			klines, err = closedBackfillKlines(klines, interval, c.now().UnixMilli())
			if err != nil {
				failures++
				slog.Warn("filter backfill klines failed", "symbol", symbol, "interval", interval, "error", err)
				continue
			}
			if len(klines) == 0 {
				continue
			}
			if err := c.store.UpsertKlines(ctx, klines); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				failures++
				slog.Warn("store backfill klines failed", "symbol", symbol, "interval", interval, "count", len(klines), "error", err)
				continue
			}
			if len(klines) > 0 {
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

func closedBackfillKlines(klines []model.Kline, interval string, now int64) ([]model.Kline, error) {
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return nil, err
	}
	closed := make([]model.Kline, 0, len(klines))
	for _, kline := range klines {
		if kline.OpenTime+intervalMillis > now {
			continue
		}
		kline.IsClosed = true
		closed = append(closed, kline)
	}
	return closed, nil
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

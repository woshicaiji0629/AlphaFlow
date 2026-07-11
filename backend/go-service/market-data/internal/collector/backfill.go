package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"alphaflow/go-service/market-data/internal/aggregator"
	"alphaflow/go-service/market-data/internal/model"
)

const defaultStartupLookback int64 = 310

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
	var allErrs []error
	var successfulSymbols int
	lookback := c.options.StartupLookback
	if lookback <= 0 {
		lookback = defaultStartupLookback
	}
	for _, symbol := range c.options.Symbols {
		var symbolErrs []error
		fetched := make(map[string][]model.Kline)
		for _, interval := range c.backfillIntervals() {
			fetchLookback, err := c.sourceLookback(ctx, symbol, interval, lookback)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				symbolErrs = append(symbolErrs, fmt.Errorf("prepare %s: %w", interval, err))
				slog.Warn("prepare backfill failed", "symbol", symbol, "interval", interval, "error", err)
				continue
			}
			if fetchLookback == lookback {
				cached, complete, cacheErr := c.recentStoredKlines(ctx, symbol, interval, lookback)
				if cacheErr != nil {
					symbolErrs = append(symbolErrs, fmt.Errorf("read cache %s: %w", interval, cacheErr))
					slog.Warn("read recent kline cache failed", "symbol", symbol, "interval", interval, "error", cacheErr)
					continue
				}
				if complete {
					fetched[backfillCacheKey(symbol, interval)] = cached
					c.rememberStoredKlines(cached, klineSourceStartupREST, false)
					continue
				}
			}
			if err := c.waitBackfillRequest(ctx); err != nil {
				return err
			}
			klines, err := c.fetchRecentKlines(ctx, symbol, interval, fetchLookback)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				symbolErrs = append(symbolErrs, fmt.Errorf("fetch %s: %w", interval, err))
				slog.Warn("backfill klines failed", "symbol", symbol, "interval", interval, "error", err)
				continue
			}
			fetched[backfillCacheKey(symbol, interval)] = klines
			stored := tailKlines(klines, lookback)
			if err := c.store.UpsertKlines(ctx, stored); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				symbolErrs = append(symbolErrs, fmt.Errorf("store %s: %w", interval, err))
				slog.Warn("store backfill klines failed", "symbol", symbol, "interval", interval, "count", len(klines), "error", err)
				continue
			}
			c.rememberStoredKlines(stored, klineSourceStartupREST, true)
			slog.Info("backfilled recent klines", "symbol", symbol, "interval", interval, "count", len(stored))
		}
		for _, rule := range c.options.StartupDerivedRules {
			if !ruleContainsSymbol(rule, symbol) {
				continue
			}
			cachedDerived, complete, err := c.recentStoredKlines(ctx, symbol, rule.TargetInterval, lookback)
			if err != nil {
				symbolErrs = append(symbolErrs, fmt.Errorf("read derived cache %s: %w", rule.TargetInterval, err))
				slog.Warn("read derived kline cache failed", "symbol", symbol, "interval", rule.TargetInterval, "error", err)
				continue
			}
			if complete {
				c.rememberStoredKlines(cachedDerived, klineSourceDerived, false)
				continue
			}
			klines, err := aggregateRecentKlines(rule, symbol, fetched[backfillCacheKey(symbol, rule.SourceInterval)], lookback, c.now().UnixMilli())
			if err != nil {
				symbolErrs = append(symbolErrs, fmt.Errorf("aggregate %s: %w", rule.TargetInterval, err))
				slog.Warn("aggregate startup klines failed", "symbol", symbol, "interval", rule.TargetInterval, "error", err)
				continue
			}
			if err := c.store.UpsertKlines(ctx, klines); err != nil {
				symbolErrs = append(symbolErrs, fmt.Errorf("store aggregate %s: %w", rule.TargetInterval, err))
				slog.Warn("store startup aggregate failed", "symbol", symbol, "interval", rule.TargetInterval, "error", err)
				continue
			}
			c.rememberStoredKlines(klines, klineSourceDerived, true)
			slog.Info("backfilled recent derived klines", "symbol", symbol, "interval", rule.TargetInterval, "count", len(klines))
		}
		symbolErr := errors.Join(symbolErrs...)
		if symbolErr != nil {
			c.setSymbolUnavailable(ctx, symbol, symbolErr.Error())
			allErrs = append(allErrs, fmt.Errorf("%s: %w", symbol, symbolErr))
			continue
		}
		c.setSymbolAvailable(ctx, symbol)
		successfulSymbols++
	}
	if successfulSymbols > 0 {
		c.setMarketAvailable(ctx)
		if len(allErrs) > 0 {
			slog.Warn("startup backfill completed with unavailable symbols", "exchange", c.rest.Exchange(), "market", c.rest.Market(), "failed", len(allErrs))
		}
		return nil
	}
	err := errors.Join(allErrs...)
	if err == nil {
		err = fmt.Errorf("no symbols configured")
	}
	c.setMarketUnavailable(ctx, err.Error())
	return err
}

func (c *Collector) fetchRecentKlines(ctx context.Context, symbol string, interval string, lookback int64) ([]model.Kline, error) {
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return nil, err
	}
	end := alignBackfillTime(c.now().UnixMilli(), intervalMillis) - intervalMillis
	start := end - (lookback-1)*intervalMillis
	limit := c.options.RESTLimit
	if int64(limit) < lookback {
		limit = int(lookback)
	}
	klines, err := c.rest.FetchKlines(ctx, symbol, interval, limit, start)
	if err != nil {
		return nil, err
	}
	klines, err = closedBackfillKlines(klines, interval, c.now().UnixMilli())
	if err != nil {
		return nil, err
	}
	byOpenTime := make(map[int64]model.Kline, len(klines))
	for _, kline := range klines {
		if kline.OpenTime < start || kline.OpenTime > end {
			continue
		}
		byOpenTime[kline.OpenTime] = kline
	}
	result := make([]model.Kline, 0, len(byOpenTime))
	for _, kline := range byOpenTime {
		result = append(result, kline)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].OpenTime < result[j].OpenTime })
	if int64(len(result)) != lookback {
		return nil, fmt.Errorf("incomplete rolling window: got %d want %d", len(result), lookback)
	}
	for index, kline := range result {
		if kline.OpenTime != start+int64(index)*intervalMillis {
			return nil, fmt.Errorf("discontinuous rolling window at %d", kline.OpenTime)
		}
	}
	return result, nil
}

func ruleContainsSymbol(rule aggregator.Rule, symbol string) bool {
	for _, item := range rule.Symbols {
		if item == symbol {
			return true
		}
	}
	return false
}

func (c *Collector) sourceLookback(ctx context.Context, symbol string, interval string, targetLookback int64) (int64, error) {
	result := targetLookback
	for _, rule := range c.options.StartupDerivedRules {
		if rule.SourceInterval != interval {
			continue
		}
		_, complete, err := c.recentStoredKlines(ctx, symbol, rule.TargetInterval, targetLookback)
		if err != nil {
			return 0, err
		}
		if complete {
			continue
		}
		sourceMillis, err := model.IntervalMillis(rule.SourceInterval)
		if err != nil {
			return 0, err
		}
		targetMillis, err := model.IntervalMillis(rule.TargetInterval)
		if err != nil {
			return 0, err
		}
		ratio := targetMillis / sourceMillis
		needed := targetLookback*ratio + ratio
		if needed > result {
			result = needed
		}
	}
	return result, nil
}

func (c *Collector) recentStoredKlines(ctx context.Context, symbol string, interval string, lookback int64) ([]model.Kline, bool, error) {
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return nil, false, err
	}
	end := alignBackfillTime(c.now().UnixMilli(), intervalMillis) - intervalMillis
	start := end - (lookback-1)*intervalMillis
	klines, err := c.store.RangeKlines(ctx, c.rest.Exchange(), c.rest.Market(), symbol, interval, start, end)
	if err != nil {
		return nil, false, err
	}
	if int64(len(klines)) != lookback {
		return klines, false, nil
	}
	sort.Slice(klines, func(i, j int) bool { return klines[i].OpenTime < klines[j].OpenTime })
	for index, kline := range klines {
		if !kline.IsClosed || kline.OpenTime != start+int64(index)*intervalMillis {
			return klines, false, nil
		}
	}
	return klines, true, nil
}

func aggregateRecentKlines(rule aggregator.Rule, symbol string, source []model.Kline, lookback int64, now int64) ([]model.Kline, error) {
	sourceMillis, err := model.IntervalMillis(rule.SourceInterval)
	if err != nil {
		return nil, err
	}
	targetMillis, err := model.IntervalMillis(rule.TargetInterval)
	if err != nil {
		return nil, err
	}
	sourceByOpenTime := make(map[int64]model.Kline, len(source))
	for _, kline := range source {
		sourceByOpenTime[kline.OpenTime] = kline
	}
	end := alignBackfillTime(now, targetMillis) - targetMillis
	start := end - (lookback-1)*targetMillis
	result := make([]model.Kline, 0, lookback)
	for openTime := start; openTime <= end; openTime += targetMillis {
		parts := make([]model.Kline, 0, 4)
		for sourceTime := openTime; sourceTime < openTime+targetMillis; {
			kline, ok := sourceByOpenTime[sourceTime]
			if !ok {
				break
			}
			parts = append(parts, kline)
			sourceTime += sourceMillis
		}
		kline, ok, err := aggregator.Aggregate(rule, symbol, openTime, parts)
		if err != nil {
			return nil, err
		}
		if ok {
			result = append(result, kline)
		}
	}
	if int64(len(result)) != lookback {
		return nil, fmt.Errorf("incomplete derived window: got %d want %d", len(result), lookback)
	}
	return result, nil
}

func tailKlines(klines []model.Kline, limit int64) []model.Kline {
	if int64(len(klines)) <= limit {
		return klines
	}
	return klines[len(klines)-int(limit):]
}

func backfillCacheKey(symbol string, interval string) string { return symbol + ":" + interval }

func alignBackfillTime(timestamp int64, intervalMillis int64) int64 {
	return timestamp - timestamp%intervalMillis
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
	intervals := c.options.BackfillIntervals
	if len(intervals) == 0 {
		intervals = c.options.Intervals
	}
	seen := make(map[string]struct{}, len(intervals))
	for _, interval := range intervals {
		seen[interval] = struct{}{}
	}

	result := make([]string, 0, len(intervals))
	for _, interval := range backfillIntervalPriority {
		if _, ok := seen[interval]; !ok {
			continue
		}
		result = append(result, interval)
		delete(seen, interval)
	}
	for _, interval := range intervals {
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

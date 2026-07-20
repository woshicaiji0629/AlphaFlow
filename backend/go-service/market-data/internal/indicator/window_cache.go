package indicator

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/indicatorcalc"
)

const indicatorWindowShardCount = 64

func (r *Runner) calculatedThrough(key string, openTime int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	lastCalculatedOpenTime, ok := r.lastCalculatedOpenTimes[key]
	return ok && lastCalculatedOpenTime >= openTime
}

func (r *Runner) rememberCalculatedOpenTime(key string, openTime int64) {
	r.mu.Lock()
	if current, ok := r.lastCalculatedOpenTimes[key]; !ok || openTime > current {
		r.lastCalculatedOpenTimes[key] = openTime
	}
	r.mu.Unlock()
}

func (r *Runner) windowForKline(
	ctx context.Context,
	rule Rule,
	kline model.Kline,
	intervalMillis int64,
) ([]model.Kline, error) {
	key := windowKey(rule.Exchange, rule.Market, kline.Symbol, kline.Interval)
	shard := r.windowShard(key)
	shard.mu.Lock()
	cached := shard.windows[key]
	var cachedLast model.Kline
	hasCached := false
	if cached != nil && len(cached.Klines()) > 0 {
		cachedLast = cached.Klines()[len(cached.Klines())-1]
		hasCached = true
	}
	if cached != nil && hasCached {
		if kline.OpenTime <= cachedLast.OpenTime {
			klines := cloneKlines(cached.Klines())
			shard.mu.Unlock()
			return klines, nil
		}
		if isContiguous(cachedLast, kline, intervalMillis) {
			if kline.IsClosed {
				cached.Append([]model.Kline{kline})
			} else {
				cached.PrepareAISourcePrefix()
			}
			klines := cloneKlines(cached.Klines())
			shard.mu.Unlock()
			return klines, nil
		}
	}
	shard.mu.Unlock()

	window, err := r.prepareKlineWindow(ctx, rule, kline.Symbol, kline.Interval, intervalMillis, kline.OpenTime)
	if err != nil {
		return nil, err
	}
	return cloneKlines(window.Klines()), nil
}

func cloneKlines(klines []model.Kline) []model.Kline {
	return append([]model.Kline(nil), klines...)
}

func (r *Runner) realtimeWindowForKline(
	ctx context.Context,
	rule Rule,
	kline model.Kline,
	intervalMillis int64,
) (*indicatorcalc.CalculationWindow, bool, error) {
	key := windowKey(rule.Exchange, rule.Market, kline.Symbol, kline.Interval)
	shard := r.windowShard(key)
	shard.mu.Lock()
	cached := shard.windows[key]
	if cached != nil && len(cached.Klines()) > 0 {
		cachedLast := cached.Klines()[len(cached.Klines())-1]
		if kline.OpenTime <= cachedLast.OpenTime {
			shard.mu.Unlock()
			return nil, false, nil
		}
		if isContiguous(cachedLast, kline, intervalMillis) {
			if !windowImmediatelyPrecedesKline(cached, kline, intervalMillis) {
				shard.mu.Unlock()
				return nil, false, nil
			}
			cached.PrepareAISourcePrefix()
			window := windowWithTemporaryKline(cached, kline, int(r.options.LookbackPeriods))
			shard.mu.Unlock()
			return window, true, nil
		}
	}
	shard.mu.Unlock()

	window, err := r.prepareKlineWindow(
		ctx,
		rule,
		kline.Symbol,
		kline.Interval,
		intervalMillis,
		kline.OpenTime,
	)
	if err != nil {
		return nil, false, err
	}
	if !windowImmediatelyPrecedesKline(window, kline, intervalMillis) {
		return nil, false, nil
	}
	return windowWithTemporaryKline(window, kline, int(r.options.LookbackPeriods)), true, nil
}

func (r *Runner) prepareKlineWindow(
	ctx context.Context,
	rule Rule,
	symbol string,
	interval string,
	intervalMillis int64,
	lastOpenTime int64,
) (*indicatorcalc.CalculationWindow, error) {
	key := windowKey(rule.Exchange, rule.Market, symbol, interval)
	shard := r.windowShard(key)
	shard.mu.Lock()
	cached := shard.windows[key]
	var cachedLastOpenTime int64
	var hasCached bool
	if cached != nil {
		cachedLastOpenTime, hasCached = cached.LastOpenTime()
	}
	if cached != nil && hasCached {
		if lastOpenTime <= cachedLastOpenTime {
			window := cached.Clone()
			shard.mu.Unlock()
			return preparedKlineWindow(window, intervalMillis)
		}
	}
	shard.mu.Unlock()

	if cached != nil && hasCached {
		klines, err := r.store.RangeKlines(
			ctx,
			rule.Exchange,
			rule.Market,
			symbol,
			interval,
			cachedLastOpenTime+intervalMillis,
			lastOpenTime,
		)
		if err != nil {
			return nil, err
		}
		shard.mu.Lock()
		cached = shard.windows[key]
		if cached != nil && len(cached.Klines()) > 0 {
			currentLastOpenTime, currentHasLast := cached.LastOpenTime()
			if currentHasLast && lastOpenTime <= currentLastOpenTime {
				window := cached.Clone()
				shard.mu.Unlock()
				return preparedKlineWindow(window, intervalMillis)
			}
			klines = normalizeIncrementalKlines(klines, currentLastOpenTime)
		}
		if len(klines) == 0 && cached != nil {
			window := cached.Clone()
			shard.mu.Unlock()
			return preparedKlineWindow(window, intervalMillis)
		}
		if len(klines) > 0 &&
			cached != nil &&
			len(cached.Klines()) > 0 &&
			isContiguous(cached.Klines()[len(cached.Klines())-1], klines[0], intervalMillis) {
			cached.Append(klines)
			window := cached.Clone()
			shard.mu.Unlock()
			return preparedKlineWindow(window, intervalMillis)
		}
		shard.mu.Unlock()
		slog.Warn(
			"indicator window gap detected, reload full window",
			"exchange", rule.Exchange,
			"market", rule.Market,
			"symbol", symbol,
			"interval", interval,
			"cached_last_open_time", cachedLastOpenTime,
			"last_open_time", lastOpenTime,
		)
	}

	start := lastOpenTime - (r.options.LookbackPeriods-1)*intervalMillis
	klines, err := r.store.RangeKlines(ctx, rule.Exchange, rule.Market, symbol, interval, start, lastOpenTime)
	if err != nil {
		return nil, err
	}
	window := newCalculationWindowFromKlines(klines, int(r.options.LookbackPeriods))
	return preparedKlineWindow(r.rememberWindow(key, window), intervalMillis)
}

func preparedKlineWindow(window *indicatorcalc.CalculationWindow, intervalMillis int64) (*indicatorcalc.CalculationWindow, error) {
	if err := validateKlineWindowContinuity(window, intervalMillis); err != nil {
		return nil, err
	}
	return window, nil
}

func validateKlineWindowContinuity(window *indicatorcalc.CalculationWindow, intervalMillis int64) error {
	if window == nil {
		return nil
	}
	klines := window.Klines()
	for index := 1; index < len(klines); index++ {
		if !isContiguous(klines[index-1], klines[index], intervalMillis) {
			return fmt.Errorf(
				"kline window gap: previous_open_time=%d current_open_time=%d",
				klines[index-1].OpenTime,
				klines[index].OpenTime,
			)
		}
	}
	return nil
}

func normalizeIncrementalKlines(klines []model.Kline, afterOpenTime int64) []model.Kline {
	if len(klines) == 0 {
		return nil
	}
	sort.SliceStable(klines, func(i int, j int) bool {
		return klines[i].OpenTime < klines[j].OpenTime
	})
	normalized := klines[:0]
	var lastOpenTime int64
	hasLast := false
	for _, kline := range klines {
		if kline.OpenTime <= afterOpenTime {
			continue
		}
		if hasLast && kline.OpenTime == lastOpenTime {
			normalized[len(normalized)-1] = kline
			continue
		}
		normalized = append(normalized, kline)
		lastOpenTime = kline.OpenTime
		hasLast = true
	}
	return normalized
}

func (r *Runner) rememberWindow(key string, window *indicatorcalc.CalculationWindow) *indicatorcalc.CalculationWindow {
	shard := r.windowShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if existing := shard.windows[key]; existing != nil {
		existingLastOpenTime, existingOK := existing.LastOpenTime()
		windowLastOpenTime, windowOK := window.LastOpenTime()
		if existingOK && (!windowOK || existingLastOpenTime > windowLastOpenTime) {
			return existing.Clone()
		}
	}
	shard.windows[key] = window
	return window.Clone()
}

func (r *Runner) cachedWindow(key string) *indicatorcalc.CalculationWindow {
	shard := r.windowShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if window := shard.windows[key]; window != nil {
		return window.Clone()
	}
	return nil
}

func (r *Runner) windowShard(key string) *indicatorWindowShard {
	hash := uint32(2166136261)
	for index := 0; index < len(key); index++ {
		hash ^= uint32(key[index])
		hash *= 16777619
	}
	return &r.windowShards[hash%indicatorWindowShardCount]
}

func windowWithTemporaryKline(window *indicatorcalc.CalculationWindow, kline model.Kline, limit int) *indicatorcalc.CalculationWindow {
	temporary := kline
	temporary.IsClosed = true
	klines := window.Klines()
	if len(klines) > 0 && klines[len(klines)-1].OpenTime == temporary.OpenTime {
		klines = append([]model.Kline(nil), klines...)
		klines[len(klines)-1] = temporary
		return newCalculationWindowFromKlines(klines, limit)
	}
	return window.RealtimePreview(temporary)
}

func newCalculationWindowFromKlines(klines []model.Kline, limit int) *indicatorcalc.CalculationWindow {
	window := indicatorcalc.NewCalculationWindowFromKlines(klines, limit)
	window.EnableBasicState()
	return window
}

func isContiguous(previous model.Kline, next model.Kline, intervalMillis int64) bool {
	if previous.CloseTime > 0 {
		return next.OpenTime == previous.CloseTime+1
	}
	return next.OpenTime == previous.OpenTime+intervalMillis
}

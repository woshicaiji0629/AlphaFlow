package indicator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"alphaflow/go-service/market-data/internal/indicatorwindow"
	"alphaflow/go-service/market-data/internal/model"
)

const indicatorWorkerCount = 8

type Store interface {
	LastOpenTime(
		ctx context.Context,
		exchange string,
		market string,
		symbol string,
		interval string,
	) (int64, bool, error)
	RangeKlines(
		ctx context.Context,
		exchange string,
		market string,
		symbol string,
		interval string,
		start int64,
		end int64,
	) ([]model.Kline, error)
	IsMarketAvailable(ctx context.Context, exchange string, market string) (bool, error)
	LastIndicatorOpenTime(
		ctx context.Context,
		exchange string,
		market string,
		symbol string,
		interval string,
	) (int64, bool, error)
	SetIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error
	SetLatestIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error
	SetIndicatorWindow(ctx context.Context, snapshot model.IndicatorWindowSnapshot) error
	SetLatestIndicatorWindow(ctx context.Context, snapshot model.IndicatorWindowSnapshot) error
	SetIndicatorRealtime(ctx context.Context, snapshot model.IndicatorRealtimeSnapshot) error
}

type Rule struct {
	Exchange  string
	Market    string
	Symbols   []string
	Intervals []string
}

type RunnerOptions struct {
	Rules            []Rule
	ScanInterval     time.Duration
	LookbackPeriods  int64
	CalculateOptions Options
}

type Runner struct {
	store                   Store
	options                 RunnerOptions
	mu                      sync.Mutex
	windows                 map[string]*CalculationWindow
	lastCalculatedOpenTimes map[string]int64
	now                     func() time.Time
}

type indicatorJob struct {
	rule     Rule
	symbol   string
	interval string
}

func NewRunner(store Store, options RunnerOptions) *Runner {
	if options.ScanInterval <= 0 {
		options.ScanInterval = 10 * time.Second
	}
	if options.LookbackPeriods <= 0 {
		options.LookbackPeriods = 200
	}
	if len(options.CalculateOptions.SMAPeriods) == 0 &&
		len(options.CalculateOptions.EMAPeriods) == 0 &&
		len(options.CalculateOptions.WMAPeriods) == 0 {
		options.CalculateOptions = DefaultOptions()
	}
	return &Runner{
		store:                   store,
		options:                 options,
		windows:                 map[string]*CalculationWindow{},
		lastCalculatedOpenTimes: map[string]int64{},
		now:                     time.Now,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	r.runOnceWithLogging(ctx)

	ticker := time.NewTicker(r.options.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.runOnceWithLogging(ctx)
		}
	}
}

func (r *Runner) RunOnce(ctx context.Context) error {
	var errs []error
	jobs := make([]indicatorJob, 0)
	for _, rule := range r.options.Rules {
		if err := validateRule(rule); err != nil {
			errs = append(errs, err)
			continue
		}
		available, err := r.store.IsMarketAvailable(ctx, rule.Exchange, rule.Market)
		if err != nil {
			errs = append(errs, fmt.Errorf("read market status %s %s: %w", rule.Exchange, rule.Market, err))
			continue
		}
		if !available {
			slog.Warn("skip indicators for unavailable market", "exchange", rule.Exchange, "market", rule.Market)
			continue
		}
		for _, symbol := range rule.Symbols {
			for _, interval := range rule.Intervals {
				jobs = append(jobs, indicatorJob{
					rule:     rule,
					symbol:   symbol,
					interval: interval,
				})
			}
		}
	}

	if len(jobs) == 0 {
		return errors.Join(errs...)
	}

	var errsMu sync.Mutex
	jobCh := make(chan indicatorJob)
	var wg sync.WaitGroup
	workers := workerCount(len(jobs))
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				if ctx.Err() != nil {
					return
				}
				if err := r.calculateSymbolInterval(ctx, job.rule, job.symbol, job.interval); err != nil {
					errsMu.Lock()
					errs = append(errs, fmt.Errorf("calculate indicators %s %s %s %s: %w",
						job.rule.Exchange,
						job.rule.Market,
						job.symbol,
						job.interval,
						err,
					))
					errsMu.Unlock()
				}
			}
		}()
	}
sendJobs:
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			break sendJobs
		case jobCh <- job:
		}
	}
	close(jobCh)
	wg.Wait()
	return errors.Join(errs...)
}

func (r *Runner) runOnceWithLogging(ctx context.Context) {
	if err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
		slog.Error("calculate indicators failed", "error", err)
	}
}

func (r *Runner) HandleKline(ctx context.Context, kline model.Kline) error {
	var errs []error
	for _, rule := range r.options.Rules {
		if !ruleMatchesKline(rule, kline) {
			continue
		}
		if err := r.calculateKline(ctx, rule, kline); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Runner) calculateKline(ctx context.Context, rule Rule, kline model.Kline) error {
	intervalMillis, err := model.IntervalMillis(kline.Interval)
	if err != nil {
		return err
	}
	window, err := r.windowForKline(ctx, rule, kline, intervalMillis)
	if err != nil {
		return err
	}
	calcWindow := window
	if !kline.IsClosed {
		calcWindow = windowWithTemporaryKline(window, kline, int(r.options.LookbackPeriods))
	}
	result, err := CalculateWindow(calcWindow, r.options.CalculateOptions)
	if err != nil {
		return err
	}
	snapshot := model.IndicatorSnapshot{
		Exchange:  rule.Exchange,
		Market:    rule.Market,
		Symbol:    kline.Symbol,
		Interval:  kline.Interval,
		OpenTime:  result.OpenTime,
		CloseTime: result.CloseTime,
		Values:    result.Values,
		Signals:   result.Signals,
		UpdatedAt: r.now().UnixMilli(),
	}
	windowSnapshot, err := r.indicatorWindowSnapshot(rule, kline.Symbol, kline.Interval, calcWindow, snapshot.UpdatedAt)
	if err != nil {
		return err
	}
	if kline.IsClosed {
		if err := r.store.SetIndicator(ctx, snapshot); err != nil {
			return err
		}
		if err := r.store.SetIndicatorWindow(ctx, windowSnapshot); err != nil {
			return err
		}
		r.rememberCalculatedOpenTime(windowKey(rule.Exchange, rule.Market, kline.Symbol, kline.Interval), snapshot.OpenTime)
		return nil
	}
	if err := r.store.SetLatestIndicator(ctx, snapshot); err != nil {
		return err
	}
	if err := r.store.SetLatestIndicatorWindow(ctx, windowSnapshot); err != nil {
		return err
	}
	return r.store.SetIndicatorRealtime(ctx, model.IndicatorRealtimeSnapshot{
		Exchange:  rule.Exchange,
		Market:    rule.Market,
		Symbol:    kline.Symbol,
		Interval:  kline.Interval,
		OpenTime:  snapshot.OpenTime,
		CloseTime: snapshot.CloseTime,
		Kline:     kline,
		Values:    snapshot.Values,
		Signals:   snapshot.Signals,
		UpdatedAt: snapshot.UpdatedAt,
	})
}

func (r *Runner) calculateSymbolInterval(ctx context.Context, rule Rule, symbol string, interval string) error {
	lastOpenTime, ok, err := r.store.LastOpenTime(ctx, rule.Exchange, rule.Market, symbol, interval)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return err
	}
	key := windowKey(rule.Exchange, rule.Market, symbol, interval)
	if r.calculatedThrough(key, lastOpenTime) {
		slog.Debug(
			"skip unchanged indicator",
			"exchange", rule.Exchange,
			"market", rule.Market,
			"symbol", symbol,
			"interval", interval,
			"open_time", lastOpenTime,
		)
		return nil
	}
	lastIndicatorOpenTime, hasLastIndicator, err := r.store.LastIndicatorOpenTime(
		ctx,
		rule.Exchange,
		rule.Market,
		symbol,
		interval,
	)
	if err != nil {
		return err
	}
	if hasLastIndicator && lastIndicatorOpenTime >= lastOpenTime {
		r.rememberCalculatedOpenTime(key, lastIndicatorOpenTime)
		slog.Debug(
			"skip unchanged indicator",
			"exchange", rule.Exchange,
			"market", rule.Market,
			"symbol", symbol,
			"interval", interval,
			"open_time", lastOpenTime,
		)
		return nil
	}
	window, err := r.updateWindow(ctx, rule, symbol, interval, intervalMillis, lastOpenTime)
	if err != nil {
		return err
	}
	result, err := CalculateWindow(window, r.options.CalculateOptions)
	if err != nil {
		return err
	}
	snapshot := model.IndicatorSnapshot{
		Exchange:  rule.Exchange,
		Market:    rule.Market,
		Symbol:    symbol,
		Interval:  interval,
		OpenTime:  result.OpenTime,
		CloseTime: result.CloseTime,
		Values:    result.Values,
		Signals:   result.Signals,
		UpdatedAt: r.now().UnixMilli(),
	}
	windowSnapshot, err := r.indicatorWindowSnapshot(rule, symbol, interval, window, snapshot.UpdatedAt)
	if err != nil {
		return err
	}
	if err := r.store.SetIndicator(ctx, snapshot); err != nil {
		return err
	}
	if err := r.store.SetIndicatorWindow(ctx, windowSnapshot); err != nil {
		return err
	}
	r.rememberCalculatedOpenTime(key, snapshot.OpenTime)
	slog.Debug(
		"calculated indicators",
		"exchange", rule.Exchange,
		"market", rule.Market,
		"symbol", symbol,
		"interval", interval,
		"open_time", result.OpenTime,
		"values", len(result.Values),
	)
	return nil
}

func (r *Runner) indicatorWindowSnapshot(
	rule Rule,
	symbol string,
	interval string,
	window *CalculationWindow,
	updatedAt int64,
) (model.IndicatorWindowSnapshot, error) {
	snapshots, err := r.indicatorSnapshotsForWindow(window)
	if err != nil {
		return model.IndicatorWindowSnapshot{}, err
	}
	result, err := indicatorwindow.Analyze(snapshots)
	if err != nil {
		return model.IndicatorWindowSnapshot{}, err
	}
	return model.IndicatorWindowSnapshot{
		Exchange:  rule.Exchange,
		Market:    rule.Market,
		Symbol:    symbol,
		Interval:  interval,
		OpenTime:  result.OpenTime,
		CloseTime: result.CloseTime,
		Version:   result.Version,
		Values:    result.Values,
		Signals:   result.Signals,
		UpdatedAt: updatedAt,
	}, nil
}

func (r *Runner) indicatorSnapshotsForWindow(
	window *CalculationWindow,
) ([]model.IndicatorSnapshot, error) {
	if window == nil {
		return nil, fmt.Errorf("nil calculation window")
	}
	closed := window.Klines()
	if len(closed) == 0 {
		return nil, fmt.Errorf("no closed klines")
	}
	lookback := 20
	if lookback > len(closed) {
		lookback = len(closed)
	}
	start := len(closed) - lookback
	snapshots := make([]model.IndicatorSnapshot, 0, lookback)
	for index := start; index < len(closed); index++ {
		result, err := Calculate(closed[:index+1], r.options.CalculateOptions)
		if err != nil {
			return nil, err
		}
		kline := closed[index]
		snapshots = append(snapshots, model.IndicatorSnapshot{
			Exchange:  kline.Exchange,
			Market:    kline.Market,
			Symbol:    kline.Symbol,
			Interval:  kline.Interval,
			OpenTime:  result.OpenTime,
			CloseTime: result.CloseTime,
			Values:    result.Values,
			Signals:   result.Signals,
			UpdatedAt: r.now().UnixMilli(),
		})
	}
	return snapshots, nil
}

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
) (*CalculationWindow, error) {
	key := windowKey(rule.Exchange, rule.Market, kline.Symbol, kline.Interval)
	r.mu.Lock()
	cached := r.windows[key]
	var cachedLast model.Kline
	hasCached := false
	if cached != nil && len(cached.Klines()) > 0 {
		cachedLast = cached.Klines()[len(cached.Klines())-1]
		hasCached = true
	}
	if cached != nil && hasCached {
		if kline.OpenTime <= cachedLast.OpenTime {
			window := cached.Clone()
			r.mu.Unlock()
			return window, nil
		}
		if isContiguous(cachedLast, kline, intervalMillis) {
			if kline.IsClosed {
				cached.Append([]model.Kline{kline})
			}
			window := cached.Clone()
			r.mu.Unlock()
			return window, nil
		}
	}
	r.mu.Unlock()

	return r.updateWindow(ctx, rule, kline.Symbol, kline.Interval, intervalMillis, kline.OpenTime)
}

func (r *Runner) updateWindow(
	ctx context.Context,
	rule Rule,
	symbol string,
	interval string,
	intervalMillis int64,
	lastOpenTime int64,
) (*CalculationWindow, error) {
	key := windowKey(rule.Exchange, rule.Market, symbol, interval)
	r.mu.Lock()
	cached := r.windows[key]
	var cachedLastOpenTime int64
	var hasCached bool
	if cached != nil {
		cachedLastOpenTime, hasCached = cached.LastOpenTime()
	}
	if cached != nil && hasCached {
		if lastOpenTime <= cachedLastOpenTime {
			window := cached.Clone()
			r.mu.Unlock()
			return window, nil
		}
	}
	r.mu.Unlock()

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
		r.mu.Lock()
		cached = r.windows[key]
		if cached != nil && len(cached.Klines()) > 0 {
			currentLastOpenTime, currentHasLast := cached.LastOpenTime()
			if currentHasLast && lastOpenTime <= currentLastOpenTime {
				window := cached.Clone()
				r.mu.Unlock()
				return window, nil
			}
			klines = normalizeIncrementalKlines(klines, currentLastOpenTime)
		}
		if len(klines) == 0 && cached != nil {
			window := cached.Clone()
			r.mu.Unlock()
			return window, nil
		}
		if len(klines) > 0 &&
			cached != nil &&
			len(cached.Klines()) > 0 &&
			isContiguous(cached.Klines()[len(cached.Klines())-1], klines[0], intervalMillis) {
			cached.Append(klines)
			window := cached.Clone()
			r.mu.Unlock()
			return window, nil
		}
		r.mu.Unlock()
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
	window := NewCalculationWindowFromKlines(klines, int(r.options.LookbackPeriods))
	return r.rememberWindow(key, window), nil
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

func (r *Runner) rememberWindow(key string, window *CalculationWindow) *CalculationWindow {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing := r.windows[key]; existing != nil {
		existingLastOpenTime, existingOK := existing.LastOpenTime()
		windowLastOpenTime, windowOK := window.LastOpenTime()
		if existingOK && (!windowOK || existingLastOpenTime > windowLastOpenTime) {
			return existing.Clone()
		}
	}
	r.windows[key] = window
	return window.Clone()
}

func workerCount(jobCount int) int {
	if jobCount < indicatorWorkerCount {
		return jobCount
	}
	return indicatorWorkerCount
}

func windowKey(exchange string, market string, symbol string, interval string) string {
	return strings.Join([]string{exchange, market, symbol, interval}, "\x00")
}

func ruleMatchesKline(rule Rule, kline model.Kline) bool {
	if rule.Exchange != kline.Exchange || rule.Market != kline.Market {
		return false
	}
	if !contains(rule.Symbols, kline.Symbol) {
		return false
	}
	return contains(rule.Intervals, kline.Interval)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func windowWithTemporaryKline(window *CalculationWindow, kline model.Kline, limit int) *CalculationWindow {
	klines := append([]model.Kline(nil), window.Klines()...)
	temporary := kline
	temporary.IsClosed = true
	if len(klines) > 0 && klines[len(klines)-1].OpenTime == temporary.OpenTime {
		klines[len(klines)-1] = temporary
	} else {
		klines = append(klines, temporary)
	}
	return NewCalculationWindowFromKlines(klines, limit)
}

func isContiguous(previous model.Kline, next model.Kline, intervalMillis int64) bool {
	if previous.CloseTime > 0 {
		return next.OpenTime == previous.CloseTime+1
	}
	return next.OpenTime == previous.OpenTime+intervalMillis
}

func validateRule(rule Rule) error {
	if rule.Exchange == "" || rule.Market == "" {
		return fmt.Errorf("invalid indicator rule: %#v", rule)
	}
	if len(rule.Symbols) == 0 || len(rule.Intervals) == 0 {
		return fmt.Errorf("indicator rule has empty symbols or intervals: %#v", rule)
	}
	return nil
}

package indicator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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
	store   Store
	options RunnerOptions
	mu      sync.Mutex
	windows map[string]*CalculationWindow
	now     func() time.Time
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
		store:   store,
		options: options,
		windows: map[string]*CalculationWindow{},
		now:     time.Now,
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
	if err := r.store.SetIndicator(ctx, snapshot); err != nil {
		return err
	}
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
	r.mu.Unlock()

	if cached != nil && hasCached {
		if lastOpenTime <= cachedLastOpenTime {
			return cached, nil
		}

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
		if len(klines) > 0 && isContiguous(cached.Klines()[len(cached.Klines())-1], klines[0], intervalMillis) {
			r.mu.Lock()
			cached.Append(klines)
			r.mu.Unlock()
			return cached, nil
		}
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
	r.mu.Lock()
	r.windows[key] = window
	r.mu.Unlock()
	return window, nil
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

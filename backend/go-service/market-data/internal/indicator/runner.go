package indicator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/marketbus"
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
	SetClosedIndicator(
		ctx context.Context,
		snapshot model.IndicatorSnapshot,
		windowSnapshot model.IndicatorWindowSnapshot,
	) error
	SetLatestIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error
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
	Rules             []Rule
	ScanInterval      time.Duration
	LookbackPeriods   int64
	CalculateOptions  indicatorcalc.Options
	Publisher         SnapshotPublisher
	PublishTTL        time.Duration
	TaskQueue         TaskQueue
	TaskBatch         int
	TaskMaxWait       time.Duration
	TaskMaxDeliveries int
	TaskWorkers       int
}

type SnapshotPublisher interface {
	PublishSnapshot(ctx context.Context, envelope marketbus.SnapshotEnvelope) (string, error)
}

type Runner struct {
	store                   Store
	options                 RunnerOptions
	mu                      sync.Mutex
	windows                 map[string]*indicatorcalc.CalculationWindow
	indicatorSnapshots      map[string][]model.IndicatorSnapshot
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
		options.CalculateOptions = indicatorcalc.DefaultOptions()
	}
	if options.TaskBatch <= 0 {
		options.TaskBatch = 32
	}
	if options.TaskMaxWait <= 0 {
		options.TaskMaxWait = time.Second
	}
	if options.TaskMaxDeliveries <= 0 {
		options.TaskMaxDeliveries = 5
	}
	if options.TaskWorkers <= 0 {
		options.TaskWorkers = workerCount(1000)
	}
	if options.PublishTTL <= 0 {
		options.PublishTTL = 30 * time.Second
	}
	return &Runner{
		store:                   store,
		options:                 options,
		windows:                 map[string]*indicatorcalc.CalculationWindow{},
		indicatorSnapshots:      map[string][]model.IndicatorSnapshot{},
		lastCalculatedOpenTimes: map[string]int64{},
		now:                     time.Now,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	if r.options.TaskQueue != nil {
		return r.runQueued(ctx)
	}
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
	jobs, err := r.dueJobs(ctx)
	if err != nil {
		errs = append(errs, err)
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

func (r *Runner) dueJobs(ctx context.Context) ([]indicatorJob, error) {
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
				if due, err := r.needsCalculation(ctx, rule, symbol, interval); err != nil {
					errs = append(errs, fmt.Errorf("check indicator task %s %s %s %s: %w",
						rule.Exchange,
						rule.Market,
						symbol,
						interval,
						err,
					))
				} else if due {
					jobs = append(jobs, indicatorJob{
						rule:     rule,
						symbol:   symbol,
						interval: interval,
					})
				}
			}
		}
	}
	return jobs, errors.Join(errs...)
}

func (r *Runner) needsCalculation(ctx context.Context, rule Rule, symbol string, interval string) (bool, error) {
	lastOpenTime, ok, err := r.store.LastOpenTime(ctx, rule.Exchange, rule.Market, symbol, interval)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	key := windowKey(rule.Exchange, rule.Market, symbol, interval)
	if r.calculatedThrough(key, lastOpenTime) {
		return false, nil
	}
	lastIndicatorOpenTime, hasLastIndicator, err := r.store.LastIndicatorOpenTime(ctx, rule.Exchange, rule.Market, symbol, interval)
	if err != nil {
		return false, err
	}
	if hasLastIndicator && lastIndicatorOpenTime >= lastOpenTime {
		r.rememberCalculatedOpenTime(key, lastIndicatorOpenTime)
		return false, nil
	}
	return true, nil
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
	result, err := indicatorcalc.CalculateWindow(calcWindow, r.options.CalculateOptions)
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
	windowSnapshot, err := r.indicatorWindowSnapshot(rule, kline.Symbol, kline.Interval, calcWindow, snapshot)
	if err != nil {
		return err
	}
	if kline.IsClosed {
		if err := r.store.SetClosedIndicator(ctx, snapshot, windowSnapshot); err != nil {
			return err
		}
		if err := r.publishClosedSnapshot(ctx, snapshot, windowSnapshot); err != nil {
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
	realtimeSnapshot := model.IndicatorRealtimeSnapshot{
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
	}
	if err := r.store.SetIndicatorRealtime(ctx, realtimeSnapshot); err != nil {
		return err
	}
	return r.publishRealtimeSnapshot(ctx, realtimeSnapshot)
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
	result, err := indicatorcalc.CalculateWindow(window, r.options.CalculateOptions)
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
	windowSnapshot, err := r.indicatorWindowSnapshot(rule, symbol, interval, window, snapshot)
	if err != nil {
		return err
	}
	if err := r.store.SetClosedIndicator(ctx, snapshot, windowSnapshot); err != nil {
		return err
	}
	if err := r.publishClosedSnapshot(ctx, snapshot, windowSnapshot); err != nil {
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

func workerCount(jobCount int) int {
	limit := runtime.NumCPU() * 2
	if limit < indicatorWorkerCount {
		limit = indicatorWorkerCount
	}
	if limit > 32 {
		limit = 32
	}
	if jobCount < limit {
		return jobCount
	}
	return limit
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

func validateRule(rule Rule) error {
	if rule.Exchange == "" || rule.Market == "" {
		return fmt.Errorf("invalid indicator rule: %#v", rule)
	}
	if len(rule.Symbols) == 0 || len(rule.Intervals) == 0 {
		return fmt.Errorf("indicator rule has empty symbols or intervals: %#v", rule)
	}
	return nil
}

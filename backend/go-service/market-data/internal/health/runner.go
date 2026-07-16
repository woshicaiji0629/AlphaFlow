package health

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
)

const (
	defaultScanInterval = 10 * time.Second
	defaultGapLookback  = 5
)

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
	SetDataHealth(ctx context.Context, health model.DataHealth) error
}

type symbolAvailabilityStore interface {
	IsSymbolAvailable(ctx context.Context, exchange string, market string, symbol string) (bool, error)
}

type Rule struct {
	Exchange  string
	Market    string
	Symbols   []string
	Intervals []string
}

type Options struct {
	Rules        []Rule
	ScanInterval time.Duration
	GapLookback  int64
	Workers      int
}

type healthJob struct {
	rule     Rule
	symbol   string
	interval string
	skipped  bool
}

type Runner struct {
	store   Store
	options Options
	now     func() time.Time
}

func NewRunner(store Store, options Options) *Runner {
	if options.ScanInterval <= 0 {
		options.ScanInterval = defaultScanInterval
	}
	if options.GapLookback <= 0 {
		options.GapLookback = defaultGapLookback
	}
	return &Runner{
		store:   store,
		options: options,
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
	jobs := make([]healthJob, 0)
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
			for _, symbol := range rule.Symbols {
				for _, interval := range rule.Intervals {
					jobs = append(jobs, healthJob{rule: rule, symbol: symbol, interval: interval, skipped: true})
				}
			}
			continue
		}
		for _, symbol := range rule.Symbols {
			symbolAvailable, err := healthSymbolAvailable(ctx, r.store, rule.Exchange, rule.Market, symbol)
			if err != nil {
				errs = append(errs, fmt.Errorf("read symbol status %s %s %s: %w", rule.Exchange, rule.Market, symbol, err))
				continue
			}
			if !symbolAvailable {
				for _, interval := range rule.Intervals {
					jobs = append(jobs, healthJob{rule: rule, symbol: symbol, interval: interval, skipped: true})
				}
				continue
			}
			for _, interval := range rule.Intervals {
				jobs = append(jobs, healthJob{rule: rule, symbol: symbol, interval: interval})
			}
		}
	}
	errs = append(errs, r.runJobs(ctx, jobs)...)
	return errors.Join(errs...)
}

func (r *Runner) runJobs(ctx context.Context, jobs []healthJob) []error {
	if len(jobs) == 0 {
		return nil
	}
	jobCh := make(chan healthJob)
	var wg sync.WaitGroup
	var errsMu sync.Mutex
	var errs []error
	workers := healthWorkerCount(len(jobs), r.options.Workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				if ctx.Err() != nil {
					return
				}
				if err := r.runJob(ctx, job); err != nil {
					errsMu.Lock()
					errs = append(errs, err)
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
	return errs
}

func (r *Runner) runJob(ctx context.Context, job healthJob) error {
	if job.skipped {
		if err := r.writeSkipped(ctx, job.rule, job.symbol, job.interval); err != nil {
			return fmt.Errorf("write skipped data health %s %s %s %s: %w", job.rule.Exchange, job.rule.Market, job.symbol, job.interval, err)
		}
		return nil
	}
	if err := r.checkSymbolInterval(ctx, job.rule, job.symbol, job.interval); err != nil {
		return fmt.Errorf("check data health %s %s %s %s: %w", job.rule.Exchange, job.rule.Market, job.symbol, job.interval, err)
	}
	return nil
}

func healthWorkerCount(jobCount int, configured int) int {
	workers := configured
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers < 1 {
		workers = 1
	}
	return min(workers, jobCount)
}

func healthSymbolAvailable(ctx context.Context, store Store, exchange string, market string, symbol string) (bool, error) {
	if symbolStore, ok := store.(symbolAvailabilityStore); ok {
		return symbolStore.IsSymbolAvailable(ctx, exchange, market, symbol)
	}
	return store.IsMarketAvailable(ctx, exchange, market)
}

func (r *Runner) runOnceWithLogging(ctx context.Context) {
	if err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
		slog.Error("check market data health failed", "error", err)
	}
}

func (r *Runner) writeSkipped(ctx context.Context, rule Rule, symbol string, interval string) error {
	return r.store.SetDataHealth(ctx, model.DataHealth{
		Exchange:        rule.Exchange,
		Market:          rule.Market,
		Symbol:          symbol,
		Interval:        interval,
		KlineStatus:     model.HealthStatusSkipped,
		IndicatorStatus: model.HealthStatusSkipped,
		Reason:          "market unavailable",
		UpdatedAt:       r.now().UnixMilli(),
	})
}

func (r *Runner) checkSymbolInterval(ctx context.Context, rule Rule, symbol string, interval string) error {
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return err
	}
	lastKlineOpenTime, hasKline, err := r.store.LastOpenTime(ctx, rule.Exchange, rule.Market, symbol, interval)
	if err != nil {
		return err
	}
	lastIndicatorOpenTime, hasIndicator, err := r.store.LastIndicatorOpenTime(ctx, rule.Exchange, rule.Market, symbol, interval)
	if err != nil {
		return err
	}

	health := model.DataHealth{
		Exchange:  rule.Exchange,
		Market:    rule.Market,
		Symbol:    symbol,
		Interval:  interval,
		UpdatedAt: r.now().UnixMilli(),
	}
	if hasKline {
		health.LastKlineOpenTime = lastKlineOpenTime
	}
	if hasIndicator {
		health.LastIndicatorOpenTime = lastIndicatorOpenTime
	}

	reasons := []string{}
	health.KlineStatus, reasons, err = r.klineStatus(ctx, rule, symbol, interval, intervalMillis, lastKlineOpenTime, hasKline, reasons)
	if err != nil {
		return err
	}
	health.IndicatorStatus, reasons = indicatorStatus(lastKlineOpenTime, hasKline, lastIndicatorOpenTime, hasIndicator, reasons)
	health.Reason = strings.Join(reasons, "; ")

	return r.store.SetDataHealth(ctx, health)
}

func (r *Runner) klineStatus(
	ctx context.Context,
	rule Rule,
	symbol string,
	interval string,
	intervalMillis int64,
	lastOpenTime int64,
	hasLast bool,
	reasons []string,
) (string, []string, error) {
	if !hasLast {
		return model.HealthStatusMissing, append(reasons, "missing kline"), nil
	}
	staleAfter := int64(2 * intervalMillis)
	if r.now().UnixMilli()-lastOpenTime > staleAfter {
		return model.HealthStatusStale, append(reasons, "kline stale"), nil
	}
	hasGap, err := r.hasRecentGap(ctx, rule, symbol, interval, intervalMillis, lastOpenTime)
	if err != nil {
		return "", reasons, err
	}
	if hasGap {
		return model.HealthStatusGap, append(reasons, "recent kline gap"), nil
	}
	return model.HealthStatusOK, reasons, nil
}

func (r *Runner) hasRecentGap(
	ctx context.Context,
	rule Rule,
	symbol string,
	interval string,
	intervalMillis int64,
	lastOpenTime int64,
) (bool, error) {
	lookback := r.options.GapLookback
	start := lastOpenTime - (lookback-1)*intervalMillis
	klines, err := r.store.RangeKlines(ctx, rule.Exchange, rule.Market, symbol, interval, start, lastOpenTime)
	if err != nil {
		return false, err
	}
	if int64(len(klines)) < lookback {
		return true, nil
	}
	for index, kline := range klines {
		wantOpenTime := start + int64(index)*intervalMillis
		if kline.OpenTime != wantOpenTime || !kline.IsClosed {
			return true, nil
		}
	}
	return false, nil
}

func indicatorStatus(
	lastKlineOpenTime int64,
	hasKline bool,
	lastIndicatorOpenTime int64,
	hasIndicator bool,
	reasons []string,
) (string, []string) {
	if !hasIndicator {
		return model.HealthStatusMissing, append(reasons, "missing indicator")
	}
	if hasKline && lastIndicatorOpenTime < lastKlineOpenTime {
		return model.HealthStatusStale, append(reasons, "indicator stale")
	}
	return model.HealthStatusOK, reasons
}

func validateRule(rule Rule) error {
	if rule.Exchange == "" || rule.Market == "" {
		return fmt.Errorf("invalid health rule: %#v", rule)
	}
	if len(rule.Symbols) == 0 || len(rule.Intervals) == 0 {
		return fmt.Errorf("health rule has empty symbols or intervals: %#v", rule)
	}
	return nil
}

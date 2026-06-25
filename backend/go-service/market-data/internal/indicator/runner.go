package indicator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/model"
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
	now     func() time.Time
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
				if err := r.calculateSymbolInterval(ctx, rule, symbol, interval); err != nil {
					errs = append(errs, fmt.Errorf("calculate indicators %s %s %s %s: %w",
						rule.Exchange,
						rule.Market,
						symbol,
						interval,
						err,
					))
				}
			}
		}
	}
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
	start := lastOpenTime - (r.options.LookbackPeriods-1)*intervalMillis
	klines, err := r.store.RangeKlines(ctx, rule.Exchange, rule.Market, symbol, interval, start, lastOpenTime)
	if err != nil {
		return err
	}
	result, err := Calculate(klines, r.options.CalculateOptions)
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

func validateRule(rule Rule) error {
	if rule.Exchange == "" || rule.Market == "" {
		return fmt.Errorf("invalid indicator rule: %#v", rule)
	}
	if len(rule.Symbols) == 0 || len(rule.Intervals) == 0 {
		return fmt.Errorf("indicator rule has empty symbols or intervals: %#v", rule)
	}
	return nil
}

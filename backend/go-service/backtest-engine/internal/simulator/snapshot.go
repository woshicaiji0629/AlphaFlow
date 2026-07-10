package simulator

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/indicatorwindow"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyframe"
)

var errSnapshotNotReady = errors.New("strategy snapshot not ready")
var calculateIndicatorWindows = indicatorcalc.CalculateWindows

type preparedSeries struct {
	klines     []marketmodel.Kline
	indicators []marketmodel.IndicatorSnapshot
	windows    []strategy.IndicatorWindowView
}

type SnapshotBuilderOptions struct {
	Dataset          reader.Dataset
	Target           strategy.Target
	Interval         string
	ConfirmIntervals []string
	IndicatorOptions indicatorcalc.Options
}

type SnapshotBuilder struct {
	dataset          reader.Dataset
	target           strategy.Target
	interval         string
	confirmIntervals []string
	indicatorOptions indicatorcalc.Options
	seriesByKey      map[reader.SeriesKey]reader.SeriesResult
	preparedByKey    map[reader.SeriesKey]preparedSeries
	prepared         bool
	prepareErr       error
}

func NewSnapshotBuilder(options SnapshotBuilderOptions) (*SnapshotBuilder, error) {
	if options.Target.Exchange == "" {
		return nil, fmt.Errorf("target exchange cannot be empty")
	}
	if options.Target.Market == "" {
		return nil, fmt.Errorf("target market cannot be empty")
	}
	if options.Target.Symbol == "" {
		return nil, fmt.Errorf("target symbol cannot be empty")
	}
	if options.Interval == "" {
		return nil, fmt.Errorf("interval cannot be empty")
	}
	if _, err := marketmodel.IntervalMillis(options.Interval); err != nil {
		return nil, err
	}
	for _, interval := range options.ConfirmIntervals {
		if _, err := marketmodel.IntervalMillis(interval); err != nil {
			return nil, err
		}
	}
	seriesByKey := make(map[reader.SeriesKey]reader.SeriesResult, len(options.Dataset.Series))
	for _, series := range options.Dataset.Series {
		seriesByKey[series.Key] = series
	}
	return &SnapshotBuilder{
		dataset:          options.Dataset,
		target:           options.Target,
		interval:         options.Interval,
		confirmIntervals: options.ConfirmIntervals,
		indicatorOptions: options.IndicatorOptions,
		seriesByKey:      seriesByKey,
		preparedByKey:    make(map[reader.SeriesKey]preparedSeries),
	}, nil
}

func (b *SnapshotBuilder) Build(ctx context.Context) ([]strategy.Context, error) {
	if err := b.prepare(ctx); err != nil {
		return nil, err
	}
	entry, err := b.series(b.target.Symbol, b.interval)
	if err != nil {
		return nil, err
	}
	contexts := []strategy.Context{}
	for _, current := range entry.Result.Klines {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if current.OpenTime < entry.Result.RequestedStart || current.OpenTime >= entry.Result.End {
			continue
		}
		item, err := b.buildAt(current)
		if err != nil {
			if errors.Is(err, errSnapshotNotReady) {
				continue
			}
			return nil, fmt.Errorf("build snapshot open_time=%d: %w", current.OpenTime, err)
		}
		contexts = append(contexts, item)
	}
	return contexts, nil
}

func (b *SnapshotBuilder) prepare(ctx context.Context) error {
	if b.prepared {
		return b.prepareErr
	}
	b.prepared = true
	for _, interval := range snapshotIntervals(b.interval, b.confirmIntervals) {
		if err := ctx.Err(); err != nil {
			b.prepareErr = err
			return err
		}
		key := reader.SeriesKey{Symbol: b.target.Symbol, Interval: interval}
		series, err := b.series(key.Symbol, key.Interval)
		if err != nil {
			b.prepareErr = err
			return err
		}
		prepared, err := prepareIndicatorSeries(series.Result.Klines, b.indicatorOptions)
		if err != nil {
			b.prepareErr = fmt.Errorf("prepare indicators %s: %w", interval, err)
			return b.prepareErr
		}
		b.preparedByKey[key] = prepared
	}
	return nil
}

func (b *SnapshotBuilder) buildAt(current marketmodel.Kline) (strategy.Context, error) {
	if !current.IsClosed {
		return strategy.Context{}, fmt.Errorf("entry kline open_time=%d is not closed", current.OpenTime)
	}
	if current.CloseTime <= 0 {
		return strategy.Context{}, fmt.Errorf("entry kline open_time=%d close_time is missing", current.OpenTime)
	}
	asOf := current.CloseTime
	intervals := snapshotIntervals(b.interval, b.confirmIntervals)
	snapshots := make(map[string]strategy.Snapshot, len(intervals))
	for _, interval := range intervals {
		snapshot, err := b.buildIntervalSnapshot(interval, asOf, interval == b.interval, current)
		if err != nil {
			return strategy.Context{}, err
		}
		snapshots[interval] = snapshot
	}
	target := b.target
	target.Interval = b.interval
	return strategyframe.BuildContext(target, snapshots, asOf, strategy.TriggerOnEntryClose)
}

func (b *SnapshotBuilder) buildIntervalSnapshot(
	interval string,
	asOf int64,
	entry bool,
	entryKline marketmodel.Kline,
) (strategy.Snapshot, error) {
	series, ok := b.preparedByKey[reader.SeriesKey{Symbol: b.target.Symbol, Interval: interval}]
	if !ok {
		return strategy.Snapshot{}, fmt.Errorf("prepared series %s %s missing", b.target.Symbol, interval)
	}
	index := sort.Search(len(series.klines), func(index int) bool {
		return series.klines[index].CloseTime > asOf
	}) - 1
	if index < 0 {
		return strategy.Snapshot{}, fmt.Errorf("%w: no closed klines for %s at as_of=%d", errSnapshotNotReady, interval, asOf)
	}
	latestIndicator := series.indicators[index]
	target := b.target
	target.Interval = interval
	current := marketmodel.Kline{}
	if entry {
		current = entryKline
	}
	indicator := strategyframe.IndicatorView(latestIndicator)
	window := series.windows[index]
	updatedAt := maxInt64(indicator.UpdatedAt, window.UpdatedAt)
	return strategy.Snapshot{
		Target:    target,
		Current:   current,
		Indicator: indicator,
		Window:    window,
		Price: strategy.PriceView{
			LastPrice: current.Close,
		},
		Health:    strategy.HealthView{OK: true, UpdatedAt: updatedAt},
		AsOf:      asOf,
		Trigger:   strategy.TriggerOnEntryClose,
		UpdatedAt: updatedAt,
	}, nil
}

func (b *SnapshotBuilder) series(symbol string, interval string) (reader.SeriesResult, error) {
	series, ok := b.seriesByKey[reader.SeriesKey{Symbol: symbol, Interval: interval}]
	if !ok {
		return reader.SeriesResult{}, fmt.Errorf("dataset series %s %s missing", symbol, interval)
	}
	return series, nil
}

func prepareIndicatorSeries(klines []marketmodel.Kline, options indicatorcalc.Options) (preparedSeries, error) {
	for _, kline := range klines {
		if !kline.IsClosed {
			return preparedSeries{}, fmt.Errorf("kline open_time=%d is not closed", kline.OpenTime)
		}
	}
	results, err := calculateIndicatorWindows(klines, 0, len(klines), options)
	if err != nil {
		return preparedSeries{}, err
	}
	if len(results) != len(klines) {
		return preparedSeries{}, fmt.Errorf("indicator results=%d do not match klines=%d", len(results), len(klines))
	}
	indicators := make([]marketmodel.IndicatorSnapshot, 0, len(results))
	for index, result := range results {
		last := klines[index]
		indicators = append(indicators, marketmodel.IndicatorSnapshot{
			Exchange:  last.Exchange,
			Market:    last.Market,
			Symbol:    last.Symbol,
			Interval:  last.Interval,
			OpenTime:  result.OpenTime,
			CloseTime: result.CloseTime,
			Values:    result.Values,
			Signals:   result.Signals,
			UpdatedAt: result.CloseTime,
		})
	}
	windows := make([]strategy.IndicatorWindowView, 0, len(indicators))
	for index := range indicators {
		start := index + 1 - indicatorwindow.DefaultLookback
		if start < 0 {
			start = 0
		}
		result, err := indicatorwindow.Analyze(indicators[start : index+1])
		if err != nil {
			return preparedSeries{}, err
		}
		window, err := strategyframe.WindowViewFromResult(result, result.CloseTime)
		if err != nil {
			return preparedSeries{}, err
		}
		windows = append(windows, window)
	}
	return preparedSeries{
		klines: append([]marketmodel.Kline(nil), klines...), indicators: indicators, windows: windows,
	}, nil
}

func snapshotIntervals(interval string, confirmIntervals []string) []string {
	intervals := make([]string, 0, len(confirmIntervals)+1)
	seen := map[string]struct{}{}
	for _, value := range append([]string{interval}, confirmIntervals...) {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		intervals = append(intervals, value)
	}
	return intervals
}

func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

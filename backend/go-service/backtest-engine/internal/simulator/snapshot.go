package simulator

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/indicatorwindow"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
)

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
	}, nil
}

func (b *SnapshotBuilder) Build(ctx context.Context) ([]strategy.Context, error) {
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
			return nil, fmt.Errorf("build snapshot open_time=%d: %w", current.OpenTime, err)
		}
		contexts = append(contexts, item)
	}
	return contexts, nil
}

func (b *SnapshotBuilder) buildAt(current marketmodel.Kline) (strategy.Context, error) {
	intervals := snapshotIntervals(b.interval, b.confirmIntervals)
	snapshots := make(map[string]strategy.Snapshot, len(intervals))
	for _, interval := range intervals {
		snapshot, err := b.buildIntervalSnapshot(interval, current.OpenTime, interval == b.interval)
		if err != nil {
			return strategy.Context{}, err
		}
		snapshots[interval] = snapshot
	}
	timeframes := timeframesFromSnapshots(snapshots)
	for interval, snapshot := range snapshots {
		snapshot.Timeframes = timeframes
		snapshots[interval] = snapshot
	}
	target := b.target
	target.Interval = b.interval
	return strategy.Context{
		Target:    target,
		Snapshots: snapshots,
	}, nil
}

func (b *SnapshotBuilder) buildIntervalSnapshot(interval string, openTime int64, entry bool) (strategy.Snapshot, error) {
	series, err := b.series(b.target.Symbol, interval)
	if err != nil {
		return strategy.Snapshot{}, err
	}
	klines := klinesUntil(series.Result.Klines, openTime)
	if len(klines) == 0 {
		return strategy.Snapshot{}, fmt.Errorf("no klines for %s at open_time=%d", interval, openTime)
	}
	indicators, err := indicatorSnapshots(klines, b.indicatorOptions)
	if err != nil {
		return strategy.Snapshot{}, fmt.Errorf("calculate indicators %s: %w", interval, err)
	}
	windowResult, err := indicatorwindow.Analyze(indicators)
	if err != nil {
		return strategy.Snapshot{}, fmt.Errorf("analyze indicator window %s: %w", interval, err)
	}
	latestIndicator := indicators[len(indicators)-1]
	target := b.target
	target.Interval = interval
	current := marketmodel.Kline{}
	if entry {
		current = klines[len(klines)-1]
	}
	indicator := strategy.IndicatorView{
		OpenTime:  latestIndicator.OpenTime,
		CloseTime: latestIndicator.CloseTime,
		Values:    latestIndicator.Values,
		Signals:   latestIndicator.Signals,
		UpdatedAt: latestIndicator.UpdatedAt,
	}
	window, err := windowView(windowResult)
	if err != nil {
		return strategy.Snapshot{}, err
	}
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

func indicatorSnapshots(klines []marketmodel.Kline, options indicatorcalc.Options) ([]marketmodel.IndicatorSnapshot, error) {
	snapshots := make([]marketmodel.IndicatorSnapshot, 0, len(klines))
	for index := range klines {
		result, err := indicatorcalc.CalculateWindow(
			indicatorcalc.NewCalculationWindowFromKlines(klines[:index+1], 0),
			options,
		)
		if err != nil {
			return nil, err
		}
		last := klines[index]
		snapshots = append(snapshots, marketmodel.IndicatorSnapshot{
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
	return snapshots, nil
}

func klinesUntil(klines []marketmodel.Kline, openTime int64) []marketmodel.Kline {
	end := 0
	for end < len(klines) && klines[end].OpenTime <= openTime {
		end++
	}
	return klines[:end]
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

func timeframesFromSnapshots(snapshots map[string]strategy.Snapshot) map[string]strategy.TimeframeSnapshot {
	timeframes := make(map[string]strategy.TimeframeSnapshot, len(snapshots))
	for interval, snapshot := range snapshots {
		timeframes[interval] = strategy.TimeframeSnapshot{
			Interval:  interval,
			Indicator: snapshot.Indicator,
			Window:    snapshot.Window,
			Health:    snapshot.Health,
			UpdatedAt: snapshot.UpdatedAt,
		}
	}
	return timeframes
}

func windowView(result indicatorwindow.Result) (strategy.IndicatorWindowView, error) {
	values, sampleCount, err := numericSeries(result.Values)
	if err != nil {
		return strategy.IndicatorWindowView{}, err
	}
	signals, err := signalSeries(result.Signals)
	if err != nil {
		return strategy.IndicatorWindowView{}, err
	}
	return strategy.IndicatorWindowView{
		OpenTime:    result.OpenTime,
		CloseTime:   result.CloseTime,
		Version:     result.Version,
		SampleCount: sampleCount,
		Values:      values,
		Signals:     signals,
		UpdatedAt:   result.CloseTime,
	}, nil
}

func numericSeries(fields map[string]string) (map[string]strategy.NumericSeries, int, error) {
	values := map[string]strategy.NumericSeries{}
	sampleCount := 0
	for field, value := range fields {
		if field == "window_sample_count" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return nil, 0, fmt.Errorf("parse window_sample_count: %w", err)
			}
			sampleCount = parsed
			continue
		}
		base, suffix := splitNumericSuffix(field)
		series := values[base]
		if err := applyNumericValue(&series, suffix, value); err != nil {
			return nil, 0, fmt.Errorf("parse %s: %w", field, err)
		}
		values[base] = series
	}
	return values, sampleCount, nil
}

func splitNumericSuffix(key string) (string, string) {
	suffixes := []string{
		"_win_range_position_pct",
		"_win_falling_count",
		"_win_rising_count",
		"_win_change_pct",
		"_win_direction",
		"_win_previous",
		"_win_latest",
		"_win_change",
		"_win_slope",
		"_win_min",
		"_win_max",
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(key, suffix) {
			return strings.TrimSuffix(key, suffix), suffix
		}
	}
	return key, "_win_latest"
}

func applyNumericValue(series *strategy.NumericSeries, suffix string, value string) error {
	if suffix == "_win_direction" {
		series.Direction = value
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return err
	}
	switch suffix {
	case "_win_latest":
		series.Latest = parsed
	case "_win_previous":
		series.Previous = parsed
	case "_win_change":
		series.Change = parsed
	case "_win_change_pct":
		series.ChangePct = parsed
	case "_win_slope":
		series.Slope = parsed
	case "_win_rising_count":
		series.RisingCount = int(parsed)
	case "_win_falling_count":
		series.FallingCount = int(parsed)
	case "_win_min":
		series.Minimum = parsed
	case "_win_max":
		series.Maximum = parsed
	case "_win_range_position_pct":
		series.RangePositionPct = parsed
	default:
		return fmt.Errorf("unsupported numeric suffix %q", suffix)
	}
	return nil
}

func signalSeries(fields map[string]string) (map[string]strategy.SignalSeries, error) {
	signals := map[string]strategy.SignalSeries{}
	for field, value := range fields {
		base, suffix := splitSignalSuffix(field)
		series := signals[base]
		if err := applySignalValue(&series, suffix, value); err != nil {
			return nil, fmt.Errorf("parse %s: %w", field, err)
		}
		signals[base] = series
	}
	return signals, nil
}

func splitSignalSuffix(key string) (string, string) {
	suffixes := []string{
		"_win_last_changed_ago",
		"_win_stable_count",
		"_win_previous",
		"_win_changed",
		"_win_latest",
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(key, suffix) {
			return strings.TrimSuffix(key, suffix), suffix
		}
	}
	return key, "_win_latest"
}

func applySignalValue(series *strategy.SignalSeries, suffix string, value string) error {
	switch suffix {
	case "_win_latest":
		series.Latest = value
	case "_win_previous":
		series.Previous = value
	case "_win_changed":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		series.Changed = parsed
	case "_win_stable_count":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		series.StableCount = parsed
	case "_win_last_changed_ago":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		series.LastChangedAgo = parsed
	default:
		return fmt.Errorf("unsupported signal suffix %q", suffix)
	}
	return nil
}

func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

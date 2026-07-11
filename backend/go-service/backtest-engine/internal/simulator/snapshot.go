package simulator

import (
	"context"
	"errors"
	"fmt"

	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/indicatorwindow"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyframe"
)

var errSnapshotNotReady = errors.New("strategy snapshot not ready")
var calculateIndicatorWindow = indicatorcalc.CalculateWindow
var analyzeIndicatorWindow = indicatorwindow.Analyze

const defaultReplayCalculationWindow = 268

type PreparationProgress func(stage string, interval string, processed int, total int)

type replaySeriesState struct {
	series          reader.SeriesResult
	cursor          int
	klineWindow     *indicatorcalc.CalculationWindow
	indicators      []marketmodel.IndicatorSnapshot
	latestIndicator marketmodel.IndicatorSnapshot
	latestWindow    strategy.IndicatorWindowView
	windowCloseTime int64
	ready           bool
}

type SnapshotBuilderOptions struct {
	Dataset           reader.Dataset
	Target            strategy.Target
	Interval          string
	ConfirmIntervals  []string
	IndicatorOptions  indicatorcalc.Options
	CalculationWindow int
	Progress          PreparationProgress
}

type SnapshotBuilder struct {
	dataset           reader.Dataset
	target            strategy.Target
	interval          string
	confirmIntervals  []string
	indicatorOptions  indicatorcalc.Options
	calculationWindow int
	progress          PreparationProgress
	seriesByKey       map[reader.SeriesKey]reader.SeriesResult
	stateByKey        map[reader.SeriesKey]*replaySeriesState
}

type ContextIterator struct {
	builder *SnapshotBuilder
	entry   reader.SeriesResult
	index   int
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
		dataset:           options.Dataset,
		target:            options.Target,
		interval:          options.Interval,
		confirmIntervals:  options.ConfirmIntervals,
		indicatorOptions:  options.IndicatorOptions,
		calculationWindow: options.CalculationWindow,
		progress:          options.Progress,
		seriesByKey:       seriesByKey,
		stateByKey:        make(map[reader.SeriesKey]*replaySeriesState),
	}, nil
}

func (b *SnapshotBuilder) Build(ctx context.Context) ([]strategy.Context, error) {
	iterator, err := b.Iterator(ctx)
	if err != nil {
		return nil, err
	}
	contexts := []strategy.Context{}
	for {
		item, ok, err := iterator.Next(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		contexts = append(contexts, item)
	}
	return contexts, nil
}

func (b *SnapshotBuilder) Iterator(ctx context.Context) (*ContextIterator, error) {
	if err := b.prepare(ctx); err != nil {
		return nil, err
	}
	entry, err := b.series(b.target.Symbol, b.interval)
	if err != nil {
		return nil, err
	}
	return &ContextIterator{builder: b, entry: entry}, nil
}

func (i *ContextIterator) Next(ctx context.Context) (strategy.Context, bool, error) {
	if i == nil || i.builder == nil {
		return strategy.Context{}, false, fmt.Errorf("context iterator is required")
	}
	for i.index < len(i.entry.Result.Klines) {
		if err := ctx.Err(); err != nil {
			return strategy.Context{}, false, err
		}
		current := i.entry.Result.Klines[i.index]
		i.index++
		if !current.IsClosed {
			return strategy.Context{}, false, fmt.Errorf("entry kline open_time=%d is not closed", current.OpenTime)
		}
		if current.CloseTime <= 0 {
			return strategy.Context{}, false, fmt.Errorf("entry kline open_time=%d close_time is missing", current.OpenTime)
		}
		if err := i.builder.advanceTo(ctx, current.CloseTime); err != nil {
			return strategy.Context{}, false, err
		}
		if current.OpenTime < i.entry.Result.RequestedStart {
			continue
		}
		if current.OpenTime >= i.entry.Result.End {
			return strategy.Context{}, false, nil
		}
		item, err := i.builder.buildAt(current)
		if errors.Is(err, errSnapshotNotReady) {
			continue
		}
		if err != nil {
			return strategy.Context{}, false, fmt.Errorf("build snapshot open_time=%d: %w", current.OpenTime, err)
		}
		return item, true, nil
	}
	return strategy.Context{}, false, nil
}

func (b *SnapshotBuilder) prepare(ctx context.Context) error {
	b.stateByKey = make(map[reader.SeriesKey]*replaySeriesState, len(b.seriesByKey))
	for _, interval := range snapshotIntervals(b.interval, b.confirmIntervals) {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := reader.SeriesKey{Symbol: b.target.Symbol, Interval: interval}
		series, err := b.series(key.Symbol, key.Interval)
		if err != nil {
			return err
		}
		calculationWindow := b.calculationWindow
		if calculationWindow <= 0 {
			calculationWindow = defaultReplayCalculationWindow
		}
		klineWindow := indicatorcalc.NewCalculationWindow(calculationWindow)
		klineWindow.EnableBasicState()
		b.stateByKey[key] = &replaySeriesState{
			series:      series,
			klineWindow: klineWindow,
			indicators:  make([]marketmodel.IndicatorSnapshot, 0, indicatorwindow.DefaultLookback),
		}
	}
	return nil
}

func (b *SnapshotBuilder) advanceTo(ctx context.Context, asOf int64) error {
	for _, interval := range snapshotIntervals(b.interval, b.confirmIntervals) {
		key := reader.SeriesKey{Symbol: b.target.Symbol, Interval: interval}
		state := b.stateByKey[key]
		if state == nil {
			return fmt.Errorf("replay state %s %s missing", b.target.Symbol, interval)
		}
		for state.cursor < len(state.series.Result.Klines) {
			if err := ctx.Err(); err != nil {
				return err
			}
			kline := state.series.Result.Klines[state.cursor]
			if kline.CloseTime > asOf {
				break
			}
			if err := b.advanceSeries(interval, state, kline); err != nil {
				return err
			}
			state.cursor++
			if b.progress != nil {
				b.progress("replay", interval, state.cursor, len(state.series.Result.Klines))
			}
		}
	}
	return nil
}

func (b *SnapshotBuilder) advanceSeries(interval string, state *replaySeriesState, kline marketmodel.Kline) error {
	if !kline.IsClosed {
		return fmt.Errorf("%s kline open_time=%d is not closed", interval, kline.OpenTime)
	}
	state.klineWindow.Append([]marketmodel.Kline{kline})
	result, err := calculateIndicatorWindow(state.klineWindow, b.indicatorOptions)
	if err != nil {
		return fmt.Errorf("calculate indicator %s open_time=%d: %w", interval, kline.OpenTime, err)
	}
	indicator := marketmodel.IndicatorSnapshot{
		Exchange: kline.Exchange, Market: kline.Market, Symbol: kline.Symbol, Interval: kline.Interval,
		OpenTime: result.OpenTime, CloseTime: result.CloseTime, Values: result.Values, Signals: result.Signals, UpdatedAt: result.CloseTime,
	}
	state.indicators = append(state.indicators, indicator)
	if len(state.indicators) > indicatorwindow.DefaultLookback {
		copy(state.indicators, state.indicators[len(state.indicators)-indicatorwindow.DefaultLookback:])
		state.indicators = state.indicators[:indicatorwindow.DefaultLookback]
	}
	state.latestIndicator = indicator
	state.ready = true
	return nil
}

func (b *SnapshotBuilder) ensureIndicatorWindow(interval string, state *replaySeriesState) error {
	if state.windowCloseTime == state.latestIndicator.CloseTime {
		return nil
	}
	windowResult, err := analyzeIndicatorWindow(state.indicators)
	if err != nil {
		return fmt.Errorf("analyze indicator window %s close_time=%d: %w", interval, state.latestIndicator.CloseTime, err)
	}
	window, err := strategyframe.WindowViewFromResult(windowResult, windowResult.CloseTime)
	if err != nil {
		return fmt.Errorf("build indicator window %s close_time=%d: %w", interval, state.latestIndicator.CloseTime, err)
	}
	state.latestWindow = window
	state.windowCloseTime = state.latestIndicator.CloseTime
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
	state := b.stateByKey[reader.SeriesKey{Symbol: b.target.Symbol, Interval: interval}]
	if state == nil || !state.ready {
		return strategy.Snapshot{}, fmt.Errorf("%w: no closed klines for %s at as_of=%d", errSnapshotNotReady, interval, asOf)
	}
	if err := b.ensureIndicatorWindow(interval, state); err != nil {
		return strategy.Snapshot{}, err
	}
	if state.latestIndicator.CloseTime > asOf || state.latestWindow.CloseTime > asOf {
		return strategy.Snapshot{}, fmt.Errorf("replay state %s exceeds as_of=%d", interval, asOf)
	}
	target := b.target
	target.Interval = interval
	current := marketmodel.Kline{}
	if entry {
		current = entryKline
	}
	indicator := strategyframe.IndicatorView(state.latestIndicator)
	window := state.latestWindow
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

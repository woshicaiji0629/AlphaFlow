package simulator

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/indicatorwindow"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyframe"
)

var errSnapshotNotReady = errors.New("strategy snapshot not ready")
var calculateIndicatorWindow = indicatorcalc.CalculateWindowNumeric

type orderedIndicatorWindowAnalyzer interface {
	AppendDenseInto(snapshot marketmodel.IndicatorSnapshot, result *indicatorwindow.Result) error
}

var newIndicatorWindowAnalyzer = func() orderedIndicatorWindowAnalyzer {
	return indicatorwindow.NewOrderedAnalyzer()
}

const defaultReplayCalculationWindow = 268
const defaultIndicatorBatchSize = 30

type PreparationProgress func(stage string, interval string, processed int, total int)

type replaySeriesState struct {
	series          reader.SeriesResult
	cursor          int
	latestIndicator marketmodel.IndicatorSnapshot
	latestWindow    strategy.IndicatorWindowView
	ready           bool
	batches         <-chan indicatorBatch
	batch           []preparedIndicator
	batchIndex      int
	next            *preparedIndicator
}

type preparedIndicator struct {
	kline     marketmodel.Kline
	indicator marketmodel.IndicatorSnapshot
	window    strategy.IndicatorWindowView
}

type indicatorBatch struct {
	items []preparedIndicator
	err   error
}

type SnapshotBuilderOptions struct {
	Dataset              reader.Dataset
	Target               strategy.Target
	Interval             string
	ConfirmIntervals     []string
	IndicatorOptions     indicatorcalc.Options
	CalculationWindow    int
	IndicatorBatchSize   int
	IndicatorConcurrency int
	IndicatorLimiter     chan struct{}
	Progress             PreparationProgress
}

type SnapshotBuilder struct {
	dataset              reader.Dataset
	target               strategy.Target
	interval             string
	confirmIntervals     []string
	indicatorOptions     indicatorcalc.Options
	calculationWindow    int
	indicatorBatchSize   int
	indicatorConcurrency int
	indicatorLimiter     chan struct{}
	progress             PreparationProgress
	seriesByKey          map[reader.SeriesKey]reader.SeriesResult
	stateByKey           map[reader.SeriesKey]*replaySeriesState
}

type ContextIterator struct {
	builder *SnapshotBuilder
	entry   reader.SeriesResult
	index   int
	cancel  context.CancelFunc
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
	if options.IndicatorLimiter != nil && cap(options.IndicatorLimiter) == 0 {
		return nil, fmt.Errorf("indicator limiter must have positive capacity")
	}
	seriesByKey := make(map[reader.SeriesKey]reader.SeriesResult, len(options.Dataset.Series))
	for _, series := range options.Dataset.Series {
		seriesByKey[series.Key] = series
	}
	return &SnapshotBuilder{
		dataset:              options.Dataset,
		target:               options.Target,
		interval:             options.Interval,
		confirmIntervals:     options.ConfirmIntervals,
		indicatorOptions:     options.IndicatorOptions,
		calculationWindow:    options.CalculationWindow,
		indicatorBatchSize:   options.IndicatorBatchSize,
		indicatorConcurrency: options.IndicatorConcurrency,
		indicatorLimiter:     options.IndicatorLimiter,
		progress:             options.Progress,
		seriesByKey:          seriesByKey,
		stateByKey:           make(map[reader.SeriesKey]*replaySeriesState),
	}, nil
}

func (b *SnapshotBuilder) Build(ctx context.Context) ([]strategy.Context, error) {
	iterator, err := b.Iterator(ctx)
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
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
	workerCtx, cancel := context.WithCancel(ctx)
	if err := b.prepare(workerCtx); err != nil {
		cancel()
		return nil, err
	}
	entry, err := b.series(b.target.Symbol, b.interval)
	if err != nil {
		cancel()
		return nil, err
	}
	return &ContextIterator{builder: b, entry: entry, cancel: cancel}, nil
}

func (i *ContextIterator) Close() {
	if i == nil || i.cancel == nil {
		return
	}
	i.cancel()
}

func (i *ContextIterator) Next(ctx context.Context) (item strategy.Context, ok bool, err error) {
	if i == nil || i.builder == nil {
		return strategy.Context{}, false, fmt.Errorf("context iterator is required")
	}
	defer func() {
		if err != nil {
			i.Close()
		}
	}()
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
			i.Close()
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
	i.Close()
	return strategy.Context{}, false, nil
}

func (b *SnapshotBuilder) prepare(ctx context.Context) error {
	b.stateByKey = make(map[reader.SeriesKey]*replaySeriesState, len(b.seriesByKey))
	intervals := snapshotIntervals(b.interval, b.confirmIntervals)
	concurrency := b.indicatorConcurrency
	if concurrency <= 0 {
		concurrency = runtime.GOMAXPROCS(0)
	}
	if concurrency > len(intervals) {
		concurrency = len(intervals)
	}
	batchSize := b.indicatorBatchSize
	if batchSize <= 0 {
		batchSize = defaultIndicatorBatchSize
	}
	semaphore := b.indicatorLimiter
	if semaphore == nil {
		semaphore = make(chan struct{}, concurrency)
	}
	for _, interval := range intervals {
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
		batches := make(chan indicatorBatch)
		b.stateByKey[key] = &replaySeriesState{
			series:  series,
			batches: batches,
		}
		go b.calculateSeries(ctx, interval, series, calculationWindow, batchSize, semaphore, batches)
	}
	return nil
}

func (b *SnapshotBuilder) calculateSeries(ctx context.Context, interval string, series reader.SeriesResult, calculationWindow, batchSize int, semaphore chan struct{}, batches chan<- indicatorBatch) {
	defer close(batches)
	window := indicatorcalc.NewCalculationWindow(calculationWindow)
	window.EnableBasicState()
	analyzer := newIndicatorWindowAnalyzer()
	viewBuilder := strategyframe.NewWindowViewBuilder()
	windowResult := indicatorwindow.Result{}
	for start := 0; start < len(series.Result.Klines); start += batchSize {
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			return
		}
		end := start + batchSize
		if end > len(series.Result.Klines) {
			end = len(series.Result.Klines)
		}
		batch := indicatorBatch{items: make([]preparedIndicator, 0, end-start)}
		for _, kline := range series.Result.Klines[start:end] {
			if !kline.IsClosed {
				batch.err = fmt.Errorf("%s kline open_time=%d is not closed", interval, kline.OpenTime)
				break
			}
			window.Append([]marketmodel.Kline{kline})
			result, err := calculateIndicatorWindow(window, b.indicatorOptions)
			if err != nil {
				batch.err = fmt.Errorf("calculate indicator %s open_time=%d: %w", interval, kline.OpenTime, err)
				break
			}
			indicator := marketmodel.IndicatorSnapshot{
				Exchange: kline.Exchange, Market: kline.Market, Symbol: kline.Symbol, Interval: kline.Interval,
				OpenTime: result.OpenTime, CloseTime: result.CloseTime, Values: result.Values, NumericValues: result.NumericValues, Signals: result.Signals, UpdatedAt: result.CloseTime,
			}
			if err := analyzer.AppendDenseInto(indicator, &windowResult); err != nil {
				batch.err = fmt.Errorf("analyze indicator window %s close_time=%d: %w", interval, indicator.CloseTime, err)
				break
			}
			windowView, err := viewBuilder.FromResult(windowResult, windowResult.CloseTime)
			if err != nil {
				batch.err = fmt.Errorf("build indicator window %s close_time=%d: %w", interval, indicator.CloseTime, err)
				break
			}
			batch.items = append(batch.items, preparedIndicator{
				kline:     kline,
				indicator: indicator,
				window:    windowView,
			})
		}
		<-semaphore
		select {
		case batches <- batch:
		case <-ctx.Done():
			return
		}
		if batch.err != nil {
			return
		}
	}
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
			prepared, err := state.peekPrepared(ctx)
			if err != nil {
				return err
			}
			if prepared.kline.CloseTime > asOf {
				break
			}
			b.advanceSeries(state, *prepared)
			state.next = nil
			state.cursor++
			if b.progress != nil {
				b.progress("replay", interval, state.cursor, len(state.series.Result.Klines))
			}
		}
	}
	return nil
}

func (b *SnapshotBuilder) advanceSeries(state *replaySeriesState, prepared preparedIndicator) {
	state.latestIndicator = prepared.indicator
	state.latestWindow = prepared.window
	state.ready = true
}

func (s *replaySeriesState) peekPrepared(ctx context.Context) (*preparedIndicator, error) {
	if s.next != nil {
		return s.next, nil
	}
	for s.batchIndex >= len(s.batch) {
		select {
		case batch, ok := <-s.batches:
			if !ok {
				return nil, fmt.Errorf("indicator preparation ended at %d of %d", s.cursor, len(s.series.Result.Klines))
			}
			if batch.err != nil {
				return nil, batch.err
			}
			s.batch = batch.items
			s.batchIndex = 0
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	s.next = &s.batch[s.batchIndex]
	s.batchIndex++
	return s.next, nil
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

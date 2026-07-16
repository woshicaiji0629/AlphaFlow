package indicatorwindow

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	model "alphaflow/go-service/pkg/marketmodel"
)

type analysisContext struct {
	values          map[string]string
	numericValues   map[string]float64
	signals         map[string]string
	points          []point
	numericAnalyzed map[string]struct{}
	signalAnalyzed  map[string]struct{}
	encodeValues    bool
	numericKeys     []string
	signalKeys      []string
	numericFields   map[string]numericOutputFields
	signalFields    map[string]signalOutputFields
	rolling         *rollingWindow
	groupedOutput   bool
	numericWindows  []NumericWindow
	signalWindows   []SignalWindow
}

type rollingNumericSlot struct {
	values      [DefaultLookback]float64
	generations [DefaultLookback]uint64
}

type rollingSignalSlot struct {
	values      [DefaultLookback]string
	generations [DefaultLookback]uint64
}

type rollingWindow struct {
	numericSlots   []rollingNumericSlot
	signalSlots    []rollingSignalSlot
	numericIndexes map[string]int
	signalIndexes  map[string]int
	rowGenerations [DefaultLookback]uint64
	next           int
	count          int
	generation     uint64
}

// OrderedAnalyzer incrementally maintains one ordered indicator window.
// It is intended to be owned by a single interval worker.
type OrderedAnalyzer struct {
	points          []point
	numericKeys     []string
	signalKeys      []string
	numericSeen     map[string]struct{}
	signalSeen      map[string]struct{}
	numericFields   map[string]numericOutputFields
	signalFields    map[string]signalOutputFields
	typedValues     map[string]string
	rolling         rollingWindow
	numericCapacity int
	signalCapacity  int
	lastOpenTime    int64
	hasLast         bool
}

func NewOrderedAnalyzer() *OrderedAnalyzer {
	return &OrderedAnalyzer{
		points:        make([]point, 0, DefaultLookback),
		numericKeys:   make([]string, 0),
		signalKeys:    make([]string, 0),
		numericSeen:   map[string]struct{}{},
		signalSeen:    map[string]struct{}{},
		numericFields: map[string]numericOutputFields{},
		signalFields:  map[string]signalOutputFields{},
		typedValues:   make(map[string]string, 8),
		rolling: rollingWindow{
			numericIndexes: map[string]int{},
			signalIndexes:  map[string]int{},
		},
	}
}

// AppendTyped appends a newer snapshot and returns the complete typed window.
func (a *OrderedAnalyzer) AppendTyped(snapshot model.IndicatorSnapshot) (Result, error) {
	return a.appendTyped(snapshot, nil, false)
}

// AppendTypedInto appends a newer snapshot and reuses the maps in result.
// The result must not be retained after the next call on the same analyzer.
func (a *OrderedAnalyzer) AppendTypedInto(snapshot model.IndicatorSnapshot, result *Result) error {
	if result == nil {
		return fmt.Errorf("reusable indicator window result is nil")
	}
	calculated, err := a.appendTyped(snapshot, result, false)
	if err != nil {
		return err
	}
	*result = calculated
	return nil
}

// AppendDenseInto appends a newer snapshot and groups the stable rolling
// numeric and signal fields into slices. Compatibility maps remain available
// for semantic fields that are not part of a generic rolling series.
// The result must not be retained after the next call on the same analyzer.
func (a *OrderedAnalyzer) AppendDenseInto(snapshot model.IndicatorSnapshot, result *Result) error {
	if result == nil {
		return fmt.Errorf("reusable dense indicator window result is nil")
	}
	calculated, err := a.appendTyped(snapshot, result, true)
	if err != nil {
		return err
	}
	*result = calculated
	return nil
}

func (a *OrderedAnalyzer) appendTyped(snapshot model.IndicatorSnapshot, reuse *Result, groupedOutput bool) (Result, error) {
	if a == nil {
		return Result{}, fmt.Errorf("ordered analyzer is nil")
	}
	if a.hasLast && snapshot.OpenTime <= a.lastOpenTime {
		return Result{}, fmt.Errorf("indicator snapshot open_time=%d is not newer than %d", snapshot.OpenTime, a.lastOpenTime)
	}
	a.lastOpenTime = snapshot.OpenTime
	a.hasLast = true
	current := pointFromSnapshot(snapshot)
	if len(a.points) == DefaultLookback {
		copy(a.points, a.points[1:])
		a.points[len(a.points)-1] = current
	} else {
		a.points = append(a.points, current)
	}
	a.updateKeys(current)
	a.rolling.append(current)
	result, err := analyzePointsModeWithSchema(
		a.points,
		true,
		a.numericKeys,
		a.signalKeys,
		a.numericFields,
		a.signalFields,
		a.numericCapacity,
		a.signalCapacity,
		&a.rolling,
		reuse,
		a.typedValues,
		groupedOutput,
	)
	if err != nil {
		return Result{}, err
	}
	if len(result.NumericValues) > a.numericCapacity {
		a.numericCapacity = len(result.NumericValues)
	}
	if len(result.Signals) > a.signalCapacity {
		a.signalCapacity = len(result.Signals)
	}
	return result, nil
}

func (a *OrderedAnalyzer) updateKeys(current point) {
	numericChanged := false
	for key := range current.numericValues {
		if _, exists := a.numericSeen[key]; exists {
			continue
		}
		a.numericSeen[key] = struct{}{}
		a.numericKeys = append(a.numericKeys, key)
		a.rolling.ensureNumericSlot(key)
		numericChanged = true
	}
	for key, value := range current.values {
		if _, exists := current.numericValues[key]; exists {
			continue
		}
		if _, exists := a.numericSeen[key]; exists {
			continue
		}
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			continue
		}
		a.numericSeen[key] = struct{}{}
		a.numericKeys = append(a.numericKeys, key)
		a.rolling.ensureNumericSlot(key)
		numericChanged = true
	}
	if numericChanged {
		sort.Strings(a.numericKeys)
	}

	signalChanged := false
	for key := range current.signals {
		if _, exists := a.signalSeen[key]; exists {
			continue
		}
		a.signalSeen[key] = struct{}{}
		a.signalKeys = append(a.signalKeys, key)
		a.rolling.ensureSignalSlot(key)
		signalChanged = true
	}
	if signalChanged {
		sort.Strings(a.signalKeys)
	}
}

func (r *rollingWindow) ensureNumericSlot(key string) int {
	if index, ok := r.numericIndexes[key]; ok {
		return index
	}
	index := len(r.numericSlots)
	r.numericIndexes[key] = index
	r.numericSlots = append(r.numericSlots, rollingNumericSlot{})
	return index
}

func (r *rollingWindow) ensureSignalSlot(key string) int {
	if index, ok := r.signalIndexes[key]; ok {
		return index
	}
	index := len(r.signalSlots)
	r.signalIndexes[key] = index
	r.signalSlots = append(r.signalSlots, rollingSignalSlot{})
	return index
}

func (r *rollingWindow) append(current point) {
	r.generation++
	position := r.next
	r.rowGenerations[position] = r.generation
	r.next = (r.next + 1) % DefaultLookback
	if r.count < DefaultLookback {
		r.count++
	}
	for key, value := range current.numericValues {
		index, ok := r.numericIndexes[key]
		if !ok {
			continue
		}
		slot := &r.numericSlots[index]
		slot.values[position] = value
		slot.generations[position] = r.generation
	}
	for key, value := range current.values {
		if _, exists := current.numericValues[key]; exists {
			continue
		}
		index, ok := r.numericIndexes[key]
		if !ok {
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			continue
		}
		slot := &r.numericSlots[index]
		slot.values[position] = parsed
		slot.generations[position] = r.generation
	}
	for key, value := range current.signals {
		index, ok := r.signalIndexes[key]
		if !ok {
			continue
		}
		slot := &r.signalSlots[index]
		slot.values[position] = value
		slot.generations[position] = r.generation
	}
}

func Analyze(snapshots []model.IndicatorSnapshot) (Result, error) {
	if len(snapshots) == 0 {
		return Result{}, fmt.Errorf("no indicator snapshots")
	}
	ordered := append([]model.IndicatorSnapshot(nil), snapshots...)
	sort.SliceStable(ordered, func(i int, j int) bool {
		return ordered[i].OpenTime < ordered[j].OpenTime
	})
	if len(ordered) > DefaultLookback {
		ordered = ordered[len(ordered)-DefaultLookback:]
	}
	return analyzeOrdered(ordered)
}

// AnalyzeOrdered analyzes snapshots already ordered by ascending OpenTime.
// Callers must not insert or update older snapshots out of order.
func AnalyzeOrdered(snapshots []model.IndicatorSnapshot) (Result, error) {
	if len(snapshots) == 0 {
		return Result{}, fmt.Errorf("no indicator snapshots")
	}
	if len(snapshots) > DefaultLookback {
		snapshots = snapshots[len(snapshots)-DefaultLookback:]
	}
	return analyzeOrdered(snapshots)
}

// AnalyzeOrderedTyped analyzes ordered snapshots without encoding numeric
// window values as strings. It returns the same complete field set through
// NumericValues for consumers that do not require the serialized format.
func AnalyzeOrderedTyped(snapshots []model.IndicatorSnapshot) (Result, error) {
	if len(snapshots) == 0 {
		return Result{}, fmt.Errorf("no indicator snapshots")
	}
	if len(snapshots) > DefaultLookback {
		snapshots = snapshots[len(snapshots)-DefaultLookback:]
	}
	return analyzeOrderedTyped(snapshots)
}

func analyzeOrdered(snapshots []model.IndicatorSnapshot) (Result, error) {
	return analyzeOrderedMode(snapshots, false)
}

func analyzeOrderedTyped(snapshots []model.IndicatorSnapshot) (Result, error) {
	return analyzeOrderedMode(snapshots, true)
}

func analyzeOrderedMode(snapshots []model.IndicatorSnapshot, typed bool) (Result, error) {
	points := make([]point, 0, len(snapshots))
	for _, snapshot := range snapshots {
		points = append(points, pointFromSnapshot(snapshot))
	}
	return analyzePointsMode(points, typed)
}

// CalculateWindows analyzes every ordered suffix once while reusing the
// snapshot ordering and point conversion work across results.
func CalculateWindows(snapshots []model.IndicatorSnapshot) ([]Result, error) {
	return CalculateWindowsContext(context.Background(), snapshots, nil)
}

// CalculateWindowsContext analyzes every ordered suffix while allowing long
// backtest preparations to be cancelled and observed.
func CalculateWindowsContext(
	ctx context.Context,
	snapshots []model.IndicatorSnapshot,
	progress func(processed int, total int),
) ([]Result, error) {
	if len(snapshots) == 0 {
		return nil, nil
	}
	ordered := append([]model.IndicatorSnapshot(nil), snapshots...)
	sort.SliceStable(ordered, func(i int, j int) bool {
		return ordered[i].OpenTime < ordered[j].OpenTime
	})
	points := make([]point, 0, len(ordered))
	for _, snapshot := range ordered {
		points = append(points, pointFromSnapshot(snapshot))
	}
	results := make([]Result, 0, len(points))
	for index := range points {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		start := index + 1 - DefaultLookback
		if start < 0 {
			start = 0
		}
		result, err := analyzePoints(points[start : index+1])
		if err != nil {
			return nil, err
		}
		results = append(results, result)
		if progress != nil {
			progress(len(results), len(points))
		}
	}
	return results, nil
}

func analyzePoints(points []point) (Result, error) {
	return analyzePointsMode(points, false)
}

func analyzePointsMode(points []point, typed bool) (Result, error) {
	return analyzePointsModeWithSchema(points, typed, nil, nil, nil, nil, 0, 0, nil, nil, nil, false)
}

func analyzePointsModeWithSchema(
	points []point,
	typed bool,
	numericKeys []string,
	signalKeys []string,
	numericFields map[string]numericOutputFields,
	signalFields map[string]signalOutputFields,
	numericCapacity int,
	signalCapacity int,
	rolling *rollingWindow,
	reuse *Result,
	typedValues map[string]string,
	groupedOutput bool,
) (Result, error) {
	if len(points) == 0 {
		return Result{}, fmt.Errorf("no indicator points")
	}
	last := points[len(points)-1]
	valueCapacity := numericCapacity
	values := map[string]string(nil)
	if typed {
		valueCapacity = 8
		values = typedValues
	}
	if values == nil {
		values = make(map[string]string, valueCapacity)
	} else {
		clear(values)
	}
	signals := map[string]string(nil)
	if reuse != nil {
		signals = reuse.Signals
	}
	if signals == nil {
		signals = make(map[string]string, signalCapacity)
	} else {
		clear(signals)
	}
	signals["window_version"] = Version
	ctx := &analysisContext{
		values:          values,
		signals:         signals,
		points:          points,
		numericAnalyzed: make(map[string]struct{}, len(numericKeys)),
		signalAnalyzed:  make(map[string]struct{}, len(signalKeys)),
		encodeValues:    !typed,
		numericKeys:     numericKeys,
		signalKeys:      signalKeys,
		numericFields:   numericFields,
		signalFields:    signalFields,
		rolling:         rolling,
		groupedOutput:   groupedOutput,
	}
	if groupedOutput && reuse != nil {
		ctx.numericWindows = reuse.NumericWindows[:0]
		ctx.signalWindows = reuse.SignalWindows[:0]
	}
	if typed {
		if reuse != nil {
			ctx.numericValues = reuse.NumericValues
		}
		if ctx.numericValues == nil {
			ctx.numericValues = make(map[string]float64, numericCapacity)
		} else {
			clear(ctx.numericValues)
		}
	}
	ctx.setNumericValue("window_sample_count", float64(len(points)), true)
	ctx.setNumericValue("window_lookback", float64(DefaultLookback), true)

	addMovingAverageWindowAnalysis(ctx)
	addMACDWindowAnalysis(ctx)
	addMomentumWindowAnalysis(ctx)
	addVolatilityWindowAnalysis(ctx)
	addTradingViewWindowAnalysis(ctx)
	addMoneyFlowWindowAnalysis(ctx)
	addStructureWindowAnalysis(ctx)
	addCandleWindowAnalysis(ctx)
	addPumpWindowAnalysis(ctx)
	addGenericWindowAnalysis(ctx)
	if typed {
		for key, value := range ctx.values {
			if _, exists := ctx.numericValues[key]; exists {
				continue
			}
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return Result{}, fmt.Errorf("parse typed window value %s: %w", key, err)
			}
			ctx.numericValues[key] = parsed
		}
		ctx.values = nil
	}

	return Result{
		OpenTime:       last.openTime,
		CloseTime:      last.closeTime,
		Version:        Version,
		Values:         ctx.values,
		NumericValues:  ctx.numericValues,
		Signals:        ctx.signals,
		NumericWindows: ctx.numericWindows,
		SignalWindows:  ctx.signalWindows,
	}, nil
}

func (ctx *analysisContext) addNumeric(keys ...string) {
	for _, key := range keys {
		if _, ok := ctx.numericAnalyzed[key]; ok {
			continue
		}
		var stats numericStats
		var ok bool
		if ctx.rolling != nil {
			stats, ok = ctx.rolling.numericStats(key)
		} else {
			stats, ok = numericStatsFromPoints(ctx.points, key)
		}
		if !ok {
			continue
		}
		ctx.numericAnalyzed[key] = struct{}{}
		if ctx.groupedOutput {
			ctx.numericWindows = append(ctx.numericWindows, numericWindowFromStats(key, stats))
		} else {
			addNumericStatsAnalysis(ctx, key, stats)
		}
	}
}

func (ctx *analysisContext) addSignals(keys ...string) {
	for _, key := range keys {
		if _, ok := ctx.signalAnalyzed[key]; ok {
			continue
		}
		var stats signalStats
		var ok bool
		if ctx.rolling != nil {
			stats, ok = ctx.rolling.signalStats(key)
		} else {
			stats, ok = signalStatsFromPoints(ctx.points, key)
		}
		if !ok {
			continue
		}
		ctx.signalAnalyzed[key] = struct{}{}
		if ctx.groupedOutput {
			ctx.signalWindows = append(ctx.signalWindows, signalWindowFromStats(key, stats))
		} else {
			addSignalStatsAnalysis(ctx, key, stats)
		}
	}
}

func numericWindowFromStats(name string, stats numericStats) NumericWindow {
	return NumericWindow{
		Name: name, Count: stats.count, Latest: stats.latest, Previous: stats.previous,
		Change: stats.change, ChangePct: stats.changePct, Slope: stats.slope,
		Direction: stats.direction, RisingCount: stats.risingCount,
		FallingCount: stats.fallingCount, StableCount: stats.stableCount,
		Minimum: stats.minimum, Maximum: stats.maximum,
		RangePositionPct: stats.rangePositionPct,
	}
}

func signalWindowFromStats(name string, stats signalStats) SignalWindow {
	return SignalWindow{
		Name: name, Count: stats.count, Latest: stats.latest, Previous: stats.previous,
		StableCount: stats.stableCount, LastChangedAgo: stats.lastChangedAgo,
	}
}

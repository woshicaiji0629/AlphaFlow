package strategyframe

import (
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/indicatorwindow"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
)

func IndicatorView(snapshot marketmodel.IndicatorSnapshot) strategy.IndicatorView {
	return strategy.IndicatorView{
		OpenTime:      snapshot.OpenTime,
		CloseTime:     snapshot.CloseTime,
		Values:        snapshot.Values,
		NumericValues: snapshot.NumericValues,
		Signals:       snapshot.Signals,
		UpdatedAt:     snapshot.UpdatedAt,
	}
}

func PriceView(indicator strategy.IndicatorView, current marketmodel.Kline) strategy.PriceView {
	price := strategy.PriceView{
		LastPrice: indicator.Values["last_price"],
		MarkPrice: indicator.Values["mark_price"],
	}
	if price.LastPrice == "" {
		price.LastPrice = current.Close
	}
	return price
}

func WindowView(snapshot marketmodel.IndicatorWindowSnapshot) (strategy.IndicatorWindowView, error) {
	values, sampleCount, err := numericSeries(snapshot.Values)
	if err != nil {
		return strategy.IndicatorWindowView{}, err
	}
	signals, err := signalSeries(snapshot.Signals)
	if err != nil {
		return strategy.IndicatorWindowView{}, err
	}
	return strategy.IndicatorWindowView{
		OpenTime:    snapshot.OpenTime,
		CloseTime:   snapshot.CloseTime,
		Version:     snapshot.Version,
		SampleCount: sampleCount,
		Values:      values,
		Signals:     signals,
		UpdatedAt:   snapshot.UpdatedAt,
	}, nil
}

func WindowViewFromResult(result indicatorwindow.Result, updatedAt int64) (strategy.IndicatorWindowView, error) {
	return windowViewFromResult(result, updatedAt, nil)
}

type fieldDescriptor struct {
	base      string
	suffix    string
	baseIndex int
	sample    bool
}

type fieldPlan struct {
	field      string
	descriptor fieldDescriptor
}

type numericWindowPlan struct {
	numericIndex   int
	stableIndex    int
	rangeIndex     int
	directionIndex int
}

type signalWindowPlan struct {
	numericStableIndex  int
	numericChangedIndex int
	signalIndex         int
}

// WindowViewBuilder caches a stable field schema for one interval worker.
// Unknown fields are discovered and cached when they first appear.
type WindowViewBuilder struct {
	schema             *strategy.IndicatorWindowSchema
	numericDescriptors map[string]fieldDescriptor
	signalDescriptors  map[string]fieldDescriptor
	numericPlan        []fieldPlan
	signalPlan         []fieldPlan
	numericScratch     []strategy.DenseNumericSeries
	numericDirections  []string
	signalScratch      []strategy.SignalSeries
	numericActive      []bool
	signalActive       []bool
	numericUsed        []int
	signalUsed         []int
	numericWindowPlans map[string]numericWindowPlan
	signalWindowPlans  map[string]signalWindowPlan
}

func NewWindowViewBuilder() *WindowViewBuilder {
	return &WindowViewBuilder{
		schema:             strategy.NewIndicatorWindowSchema(),
		numericDescriptors: map[string]fieldDescriptor{},
		signalDescriptors:  map[string]fieldDescriptor{},
		numericWindowPlans: map[string]numericWindowPlan{},
		signalWindowPlans:  map[string]signalWindowPlan{},
	}
}

func (b *WindowViewBuilder) FromResult(result indicatorwindow.Result, updatedAt int64) (strategy.IndicatorWindowView, error) {
	if b == nil {
		return strategy.IndicatorWindowView{}, fmt.Errorf("window view builder is nil")
	}
	return windowViewFromResult(result, updatedAt, b)
}

func (b *WindowViewBuilder) FromSnapshot(snapshot marketmodel.IndicatorWindowSnapshot) (strategy.IndicatorWindowView, error) {
	return b.FromResult(indicatorwindow.Result{
		OpenTime:      snapshot.OpenTime,
		CloseTime:     snapshot.CloseTime,
		Version:       snapshot.Version,
		Values:        snapshot.Values,
		NumericValues: snapshot.NumericValues,
		Signals:       snapshot.Signals,
	}, snapshot.UpdatedAt)
}

func windowViewFromResult(result indicatorwindow.Result, updatedAt int64, builder *WindowViewBuilder) (strategy.IndicatorWindowView, error) {
	if updatedAt == 0 {
		updatedAt = result.CloseTime
	}
	if builder != nil {
		return builder.denseViewFromResult(result, updatedAt)
	}
	values := map[string]strategy.NumericSeries{}
	sampleCount := 0
	var err error
	if len(result.NumericValues) > 0 {
		values, sampleCount, err = numericSeriesFromTypedCached(result.NumericValues, builder)
	} else {
		values, sampleCount, err = numericSeriesCached(result.Values, builder)
	}
	if err != nil {
		return strategy.IndicatorWindowView{}, err
	}
	signals, err := signalSeriesCached(result.Signals, builder)
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
		UpdatedAt:   updatedAt,
	}, nil
}

func (b *WindowViewBuilder) denseViewFromResult(result indicatorwindow.Result, updatedAt int64) (strategy.IndicatorWindowView, error) {
	values, directions, numericPresent, sampleCount, err := b.numericDenseFromResult(result)
	if err != nil {
		return strategy.IndicatorWindowView{}, err
	}
	signals, signalPresent, err := b.signalDenseFromResult(result)
	if err != nil {
		return strategy.IndicatorWindowView{}, err
	}
	return strategy.IndicatorWindowView{
		OpenTime:        result.OpenTime,
		CloseTime:       result.CloseTime,
		Version:         result.Version,
		SampleCount:     sampleCount,
		Schema:          b.schema,
		DenseValues:     values,
		DenseDirections: directions,
		DenseSignals:    signals,
		NumericPresent:  numericPresent,
		SignalPresent:   signalPresent,
		UpdatedAt:       updatedAt,
	}, nil
}

func (b *WindowViewBuilder) numericDenseFromResult(result indicatorwindow.Result) ([]strategy.DenseNumericSeries, []string, []uint64, int, error) {
	b.resetNumericScratch()
	for _, window := range result.NumericWindows {
		plan := b.numericWindowPlan(window.Name)
		b.markNumericUsed(plan.numericIndex)
		b.numericScratch[plan.numericIndex] = strategy.DenseNumericSeries{
			Latest: window.Latest, Previous: window.Previous, Change: window.Change,
			ChangePct: window.ChangePct, Slope: window.Slope,
			RisingCount: window.RisingCount, FallingCount: window.FallingCount,
			Minimum: window.Minimum, Maximum: window.Maximum,
		}
		if window.Maximum != window.Minimum {
			b.markNumericUsed(plan.rangeIndex)
			b.numericScratch[plan.rangeIndex].Latest = window.RangePositionPct
		}
		b.markNumericUsed(plan.stableIndex)
		b.numericScratch[plan.stableIndex].Latest = float64(window.StableCount)
	}
	for _, window := range result.SignalWindows {
		plan := b.signalWindowPlan(window.Name)
		b.markNumericUsed(plan.numericStableIndex)
		b.numericScratch[plan.numericStableIndex].Latest = float64(window.StableCount)
		b.markNumericUsed(plan.numericChangedIndex)
		b.numericScratch[plan.numericChangedIndex].Latest = float64(window.LastChangedAgo)
	}
	sampleCount := 0
	var err error
	if len(result.NumericValues) > 0 {
		sampleCount, err = b.applyNumericNumbers(result.NumericValues)
	} else if len(result.Values) > 0 {
		sampleCount, err = b.applyNumericStrings(result.Values)
	}
	if err != nil {
		return nil, nil, nil, 0, err
	}
	values, directions, present := b.numericDenseResult()
	return values, directions, present, sampleCount, nil
}

func (b *WindowViewBuilder) numericWindowPlan(name string) numericWindowPlan {
	if plan, ok := b.numericWindowPlans[name]; ok {
		return plan
	}
	plan := numericWindowPlan{
		numericIndex:   b.ensureNumericBase(name),
		stableIndex:    b.ensureNumericBase(name + "_win_stable_count"),
		rangeIndex:     b.ensureNumericBase(name + "_win_range_pos_pct"),
		directionIndex: b.ensureSignalBase(name + "_win_direction"),
	}
	b.numericWindowPlans[name] = plan
	return plan
}

func (b *WindowViewBuilder) signalWindowPlan(name string) signalWindowPlan {
	if plan, ok := b.signalWindowPlans[name]; ok {
		return plan
	}
	plan := signalWindowPlan{
		numericStableIndex:  b.ensureNumericBase(name + "_win_stable_count"),
		numericChangedIndex: b.ensureNumericBase(name + "_win_last_changed_ago"),
		signalIndex:         b.ensureSignalBase(name),
	}
	b.signalWindowPlans[name] = plan
	return plan
}

func numericSeries(fields map[string]string) (map[string]strategy.NumericSeries, int, error) {
	return numericSeriesCached(fields, nil)
}

func numericSeriesCached(fields map[string]string, builder *WindowViewBuilder) (map[string]strategy.NumericSeries, int, error) {
	values := make(map[string]strategy.NumericSeries)
	sampleCount := 0
	for field, value := range fields {
		descriptor := describeNumericField(field, builder)
		if descriptor.sample {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return nil, 0, fmt.Errorf("parse %s: %w", field, err)
			}
			sampleCount = parsed
			continue
		}
		series := values[descriptor.base]
		if err := applyNumericValue(&series, descriptor.suffix, value); err != nil {
			return nil, 0, fmt.Errorf("parse %s: %w", field, err)
		}
		values[descriptor.base] = series
	}
	return values, sampleCount, nil
}

func numericSeriesFromTyped(fields map[string]float64) (map[string]strategy.NumericSeries, int, error) {
	return numericSeriesFromTypedCached(fields, nil)
}

func numericSeriesFromTypedCached(fields map[string]float64, builder *WindowViewBuilder) (map[string]strategy.NumericSeries, int, error) {
	values := make(map[string]strategy.NumericSeries)
	sampleCount := 0
	for field, value := range fields {
		descriptor := describeNumericField(field, builder)
		if descriptor.sample {
			sampleCount = int(value)
			continue
		}
		series := values[descriptor.base]
		if err := applyNumericNumber(&series, descriptor.suffix, value); err != nil {
			return nil, 0, fmt.Errorf("apply %s: %w", field, err)
		}
		values[descriptor.base] = series
	}
	return values, sampleCount, nil
}

func describeNumericField(field string, builder *WindowViewBuilder) fieldDescriptor {
	if builder != nil {
		if descriptor, ok := builder.numericDescriptors[field]; ok {
			return descriptor
		}
	}
	descriptor := fieldDescriptor{sample: field == "sample_count" || field == "window_sample_count"}
	if !descriptor.sample {
		descriptor.base, descriptor.suffix = splitNumericSuffix(field)
	}
	if builder != nil {
		if !descriptor.sample {
			descriptor.baseIndex = builder.ensureNumericBase(descriptor.base)
		}
		builder.numericDescriptors[field] = descriptor
		builder.numericPlan = append(builder.numericPlan, fieldPlan{field: field, descriptor: descriptor})
	}
	return descriptor
}

func (b *WindowViewBuilder) ensureNumericBase(base string) int {
	index := b.schema.EnsureNumeric(base)
	for len(b.numericScratch) <= index {
		b.numericScratch = append(b.numericScratch, strategy.DenseNumericSeries{})
		b.numericDirections = append(b.numericDirections, "")
		b.numericActive = append(b.numericActive, false)
	}
	return index
}

func (b *WindowViewBuilder) resetNumericScratch() {
	for _, index := range b.numericUsed {
		b.numericScratch[index] = strategy.DenseNumericSeries{}
		b.numericDirections[index] = ""
		b.numericActive[index] = false
	}
	b.numericUsed = b.numericUsed[:0]
}

func (b *WindowViewBuilder) markNumericUsed(index int) {
	if b.numericActive[index] {
		return
	}
	b.numericActive[index] = true
	b.numericUsed = append(b.numericUsed, index)
}

func (b *WindowViewBuilder) numericDenseFromNumbers(fields map[string]float64) ([]strategy.DenseNumericSeries, []string, []uint64, int, error) {
	b.resetNumericScratch()
	sampleCount, err := b.applyNumericNumbers(fields)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	values, directions, present := b.numericDenseResult()
	return values, directions, present, sampleCount, nil
}

func (b *WindowViewBuilder) applyNumericNumbers(fields map[string]float64) (int, error) {
	sampleCount := 0
	matched := 0
	for _, plan := range b.numericPlan {
		value, ok := fields[plan.field]
		if !ok {
			continue
		}
		matched++
		if plan.descriptor.sample {
			sampleCount = int(value)
			continue
		}
		b.markNumericUsed(plan.descriptor.baseIndex)
		if err := applyDenseNumericNumber(&b.numericScratch[plan.descriptor.baseIndex], plan.descriptor.suffix, value); err != nil {
			return 0, fmt.Errorf("apply %s: %w", plan.field, err)
		}
	}
	if matched < len(fields) {
		for field, value := range fields {
			if _, known := b.numericDescriptors[field]; known {
				continue
			}
			descriptor := describeNumericField(field, b)
			if descriptor.sample {
				sampleCount = int(value)
				continue
			}
			b.markNumericUsed(descriptor.baseIndex)
			if err := applyDenseNumericNumber(&b.numericScratch[descriptor.baseIndex], descriptor.suffix, value); err != nil {
				return 0, fmt.Errorf("apply %s: %w", field, err)
			}
		}
	}
	return sampleCount, nil
}

func (b *WindowViewBuilder) numericDenseFromStrings(fields map[string]string) ([]strategy.DenseNumericSeries, []string, []uint64, int, error) {
	b.resetNumericScratch()
	sampleCount, err := b.applyNumericStrings(fields)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	values, directions, present := b.numericDenseResult()
	return values, directions, present, sampleCount, nil
}

func (b *WindowViewBuilder) applyNumericStrings(fields map[string]string) (int, error) {
	sampleCount := 0
	matched := 0
	for _, plan := range b.numericPlan {
		value, ok := fields[plan.field]
		if !ok {
			continue
		}
		matched++
		if plan.descriptor.sample {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return 0, fmt.Errorf("parse %s: %w", plan.field, err)
			}
			sampleCount = parsed
			continue
		}
		b.markNumericUsed(plan.descriptor.baseIndex)
		if err := applyDenseNumericValue(&b.numericScratch[plan.descriptor.baseIndex], &b.numericDirections[plan.descriptor.baseIndex], plan.descriptor.suffix, value); err != nil {
			return 0, fmt.Errorf("parse %s: %w", plan.field, err)
		}
	}
	if matched < len(fields) {
		for field, value := range fields {
			if _, known := b.numericDescriptors[field]; known {
				continue
			}
			descriptor := describeNumericField(field, b)
			if descriptor.sample {
				parsed, err := strconv.Atoi(value)
				if err != nil {
					return 0, fmt.Errorf("parse %s: %w", field, err)
				}
				sampleCount = parsed
				continue
			}
			b.markNumericUsed(descriptor.baseIndex)
			if err := applyDenseNumericValue(&b.numericScratch[descriptor.baseIndex], &b.numericDirections[descriptor.baseIndex], descriptor.suffix, value); err != nil {
				return 0, fmt.Errorf("parse %s: %w", field, err)
			}
		}
	}
	return sampleCount, nil
}

func (b *WindowViewBuilder) numericDenseResult() ([]strategy.DenseNumericSeries, []string, []uint64) {
	if len(b.numericUsed) == 0 {
		return nil, nil, nil
	}
	values := make([]strategy.DenseNumericSeries, len(b.numericScratch))
	var directions []string
	present := make([]uint64, (len(values)+63)/64)
	for _, index := range b.numericUsed {
		values[index] = b.numericScratch[index]
		if direction := b.numericDirections[index]; direction != "" {
			if directions == nil {
				directions = make([]string, len(b.numericScratch))
			}
			directions[index] = direction
		}
		present[index/64] |= uint64(1) << uint(index%64)
	}
	return values, directions, present
}

func splitNumericSuffix(key string) (string, string) {
	suffixes := []string{
		"_win_range_position_pct", "_win_falling_count", "_win_rising_count",
		"_win_change_pct", "_win_direction", "_win_previous", "_win_latest",
		"_win_change", "_win_slope", "_win_min", "_win_max",
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
	return applyNumericNumber(series, suffix, parsed)
}

func applyNumericNumber(series *strategy.NumericSeries, suffix string, parsed float64) error {
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

func applyDenseNumericValue(series *strategy.DenseNumericSeries, direction *string, suffix string, value string) error {
	if suffix == "_win_direction" {
		*direction = value
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return err
	}
	return applyDenseNumericNumber(series, suffix, parsed)
}

func applyDenseNumericNumber(series *strategy.DenseNumericSeries, suffix string, parsed float64) error {
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
	return signalSeriesCached(fields, nil)
}

func signalSeriesCached(fields map[string]string, builder *WindowViewBuilder) (map[string]strategy.SignalSeries, error) {
	signals := make(map[string]strategy.SignalSeries)
	for field, value := range fields {
		descriptor := describeSignalField(field, builder)
		series := signals[descriptor.base]
		if err := applySignalValue(&series, descriptor.suffix, value); err != nil {
			return nil, fmt.Errorf("parse %s: %w", field, err)
		}
		signals[descriptor.base] = series
	}
	return signals, nil
}

func describeSignalField(field string, builder *WindowViewBuilder) fieldDescriptor {
	if builder != nil {
		if descriptor, ok := builder.signalDescriptors[field]; ok {
			return descriptor
		}
	}
	base, suffix := splitSignalSuffix(field)
	descriptor := fieldDescriptor{base: base, suffix: suffix}
	if builder != nil {
		descriptor.baseIndex = builder.ensureSignalBase(descriptor.base)
		builder.signalDescriptors[field] = descriptor
		builder.signalPlan = append(builder.signalPlan, fieldPlan{field: field, descriptor: descriptor})
	}
	return descriptor
}

func (b *WindowViewBuilder) ensureSignalBase(base string) int {
	index := b.schema.EnsureSignal(base)
	for len(b.signalScratch) <= index {
		b.signalScratch = append(b.signalScratch, strategy.SignalSeries{})
		b.signalActive = append(b.signalActive, false)
	}
	return index
}

func (b *WindowViewBuilder) resetSignalScratch() {
	for _, index := range b.signalUsed {
		b.signalScratch[index] = strategy.SignalSeries{}
		b.signalActive[index] = false
	}
	b.signalUsed = b.signalUsed[:0]
}

func (b *WindowViewBuilder) markSignalUsed(index int) {
	if b.signalActive[index] {
		return
	}
	b.signalActive[index] = true
	b.signalUsed = append(b.signalUsed, index)
}

func (b *WindowViewBuilder) signalDenseFromResult(result indicatorwindow.Result) ([]strategy.DenseSignalSeries, []uint64, error) {
	b.resetSignalScratch()
	for _, window := range result.NumericWindows {
		plan := b.numericWindowPlan(window.Name)
		b.markSignalUsed(plan.directionIndex)
		b.signalScratch[plan.directionIndex].Latest = window.Direction
	}
	for _, window := range result.SignalWindows {
		plan := b.signalWindowPlan(window.Name)
		b.markSignalUsed(plan.signalIndex)
		b.signalScratch[plan.signalIndex] = strategy.SignalSeries{
			Latest: window.Latest, Previous: window.Previous,
			Changed: window.Count > 1 && window.Latest != window.Previous,
		}
	}
	if err := b.applySignalFields(result.Signals); err != nil {
		return nil, nil, err
	}
	return b.signalDenseResult()
}

func (b *WindowViewBuilder) signalDense(fields map[string]string) ([]strategy.DenseSignalSeries, []uint64, error) {
	b.resetSignalScratch()
	if err := b.applySignalFields(fields); err != nil {
		return nil, nil, err
	}
	return b.signalDenseResult()
}

func (b *WindowViewBuilder) applySignalFields(fields map[string]string) error {
	matched := 0
	for _, plan := range b.signalPlan {
		value, ok := fields[plan.field]
		if !ok {
			continue
		}
		matched++
		b.markSignalUsed(plan.descriptor.baseIndex)
		if err := applySignalValue(&b.signalScratch[plan.descriptor.baseIndex], plan.descriptor.suffix, value); err != nil {
			return fmt.Errorf("parse %s: %w", plan.field, err)
		}
	}
	if matched < len(fields) {
		for field, value := range fields {
			if _, known := b.signalDescriptors[field]; known {
				continue
			}
			descriptor := describeSignalField(field, b)
			b.markSignalUsed(descriptor.baseIndex)
			if err := applySignalValue(&b.signalScratch[descriptor.baseIndex], descriptor.suffix, value); err != nil {
				return fmt.Errorf("parse %s: %w", field, err)
			}
		}
	}
	return nil
}

func (b *WindowViewBuilder) signalDenseResult() ([]strategy.DenseSignalSeries, []uint64, error) {
	if len(b.signalUsed) == 0 {
		return nil, nil, nil
	}
	signals := make([]strategy.DenseSignalSeries, len(b.signalScratch))
	present := make([]uint64, (len(signals)+63)/64)
	for _, index := range b.signalUsed {
		signals[index] = b.signalScratch[index]
		present[index/64] |= uint64(1) << uint(index%64)
	}
	return signals, present, nil
}

func splitSignalSuffix(key string) (string, string) {
	suffixes := []string{
		"_win_last_changed_ago", "_win_stable_count", "_win_previous",
		"_win_changed", "_win_latest",
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

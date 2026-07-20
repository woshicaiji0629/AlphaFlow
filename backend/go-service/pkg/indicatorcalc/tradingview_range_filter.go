package indicatorcalc

import "math"

type streamRangeFilterState struct {
	period        int
	multiplier    float64
	rangeEMA      streamEMAState
	previousClose float64
	sampleCount   int
	filter        float64
	direction     string
}

func newStreamRangeFilterState(period int, multiplier float64) streamRangeFilterState {
	return streamRangeFilterState{
		period:     period,
		multiplier: multiplier,
		rangeEMA:   *newStreamEMAState(period),
		direction:  "flat",
	}
}

func (s *streamRangeFilterState) append(closeValue float64) {
	if s == nil || s.period <= 0 || s.multiplier <= 0 {
		return
	}
	s.sampleCount++
	if s.sampleCount == 1 {
		s.previousClose = closeValue
		return
	}
	s.rangeEMA.append(math.Abs(closeValue - s.previousClose))
	s.previousClose = closeValue
	if s.sampleCount < s.period+1 || !s.rangeEMA.ready {
		return
	}
	if s.sampleCount == s.period+1 {
		s.filter = closeValue
		return
	}
	smoothRange := s.rangeEMA.value * s.multiplier
	previous := s.filter
	switch {
	case closeValue > previous:
		s.filter = math.Max(previous, closeValue-smoothRange)
	case closeValue < previous:
		s.filter = math.Min(previous, closeValue+smoothRange)
	}
	switch {
	case s.filter > previous:
		s.direction = "up"
	case s.filter < previous:
		s.direction = "down"
	}
}

func (s *streamRangeFilterState) value() (float64, float64, float64, string, bool) {
	if s == nil || s.sampleCount < s.period+2 || !s.rangeEMA.ready {
		return 0, 0, 0, "", false
	}
	smoothRange := s.rangeEMA.value * s.multiplier
	return s.filter, s.filter + smoothRange, s.filter - smoothRange, s.direction, true
}

func addRangeFilterFeatures(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, period int, multiplier float64, basic *basicIndicatorState) {
	filter, upper, lower, direction, ok := basic.rangeFilterValue(period, multiplier)
	if !ok {
		filter, upper, lower, direction, ok = rangeFilterCompact(closes, period, multiplier)
	}
	if !ok {
		filter, upper, lower, direction, ok = rangeFilter(closes, period, multiplier)
	}
	if !ok {
		return
	}
	setValueTarget(target, values, "range_filter", filter, true)
	setValueTarget(target, values, "range_filter_upper", upper, true)
	setValueTarget(target, values, "range_filter_lower", lower, true)
	setValueTarget(target, values, "range_filter_distance_pct", percentDistance(closes[len(closes)-1], filter), filter != 0)
	signals["range_filter_direction"] = direction
}

func rangeFilterCompact(closes []float64, period int, multiplier float64) (float64, float64, float64, string, bool) {
	if period <= 0 || multiplier <= 0 || len(closes) < period+2 {
		return 0, 0, 0, "", false
	}
	rangeEMA := newStreamEMAState(period)
	filter := closes[period]
	direction := "flat"
	for index := 1; index < len(closes); index++ {
		rangeEMA.append(math.Abs(closes[index] - closes[index-1]))
		if index <= period || !rangeEMA.ready {
			continue
		}
		smoothRange := rangeEMA.value * multiplier
		previous := filter
		switch {
		case closes[index] > previous:
			filter = math.Max(previous, closes[index]-smoothRange)
		case closes[index] < previous:
			filter = math.Min(previous, closes[index]+smoothRange)
		}
		switch {
		case filter > previous:
			direction = "up"
		case filter < previous:
			direction = "down"
		}
	}
	smoothRange := rangeEMA.value * multiplier
	return filter, filter + smoothRange, filter - smoothRange, direction, true
}

func rangeFilter(closes []float64, period int, multiplier float64) (float64, float64, float64, string, bool) {
	if period <= 0 || multiplier <= 0 || len(closes) < period+2 {
		return 0, 0, 0, "", false
	}
	ranges := make([]float64, 0, len(closes)-1)
	for index := 1; index < len(closes); index++ {
		ranges = append(ranges, math.Abs(closes[index]-closes[index-1]))
	}
	smoothRangeSeries, ok := emaSeries(ranges, period)
	if !ok || len(smoothRangeSeries) == 0 {
		return 0, 0, 0, "", false
	}
	filter := closes[period]
	direction := "flat"
	for index := period + 1; index < len(closes); index++ {
		rangeIndex := index - period
		if rangeIndex >= len(smoothRangeSeries) {
			rangeIndex = len(smoothRangeSeries) - 1
		}
		smoothRange := smoothRangeSeries[rangeIndex] * multiplier
		previous := filter
		switch {
		case closes[index] > previous:
			filter = math.Max(previous, closes[index]-smoothRange)
		case closes[index] < previous:
			filter = math.Min(previous, closes[index]+smoothRange)
		}
		switch {
		case filter > previous:
			direction = "up"
		case filter < previous:
			direction = "down"
		}
	}
	smoothRange := smoothRangeSeries[len(smoothRangeSeries)-1] * multiplier
	return filter, filter + smoothRange, filter - smoothRange, direction, true
}

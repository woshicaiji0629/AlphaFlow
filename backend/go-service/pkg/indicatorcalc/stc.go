package indicatorcalc

import "math"

const (
	stcFastPeriod      = 23
	stcSlowPeriod      = 50
	stcCyclePeriod     = 10
	stcSmoothingPeriod = 3
	stcOversold        = 25.0
	stcOverbought      = 75.0
)

type stcRing struct {
	values [stcCyclePeriod]float64
	next   int
	count  int
}

func (r *stcRing) append(value float64) {
	r.values[r.next] = value
	r.next = (r.next + 1) % len(r.values)
	if r.count < len(r.values) {
		r.count++
	}
}

func (r stcRing) bounds() (float64, float64, bool) {
	if r.count < len(r.values) {
		return 0, 0, false
	}
	lowest := r.values[0]
	highest := r.values[0]
	for index := 1; index < len(r.values); index++ {
		lowest = math.Min(lowest, r.values[index])
		highest = math.Max(highest, r.values[index])
	}
	return lowest, highest, true
}

type streamSTCState struct {
	fastEMA            streamEMAState
	slowEMA            streamEMAState
	macdWindow         stcRing
	firstSmoothing     streamEMAState
	firstWindow        stcRing
	secondSmoothing    streamEMAState
	previousRaw        float64
	previousRawReady   bool
	previousCycle      float64
	previousCycleReady bool
	previous           float64
	current            float64
	pointCount         int
}

func newStreamSTCState() streamSTCState {
	return streamSTCState{
		fastEMA:         *newStreamEMAState(stcFastPeriod),
		slowEMA:         *newStreamEMAState(stcSlowPeriod),
		firstSmoothing:  *newStreamEMAState(stcSmoothingPeriod),
		secondSmoothing: *newStreamEMAState(stcSmoothingPeriod),
	}
}

func (s *streamSTCState) append(closeValue float64) {
	if s == nil {
		return
	}
	s.fastEMA.append(closeValue)
	s.slowEMA.append(closeValue)
	if !s.fastEMA.ready || !s.slowEMA.ready {
		return
	}

	macdValue := s.fastEMA.value - s.slowEMA.value
	s.macdWindow.append(macdValue)
	low, high, ok := s.macdWindow.bounds()
	if !ok {
		return
	}
	firstRaw := stochasticValue(macdValue, low, high, s.previousRaw, s.previousRawReady)
	s.previousRaw = firstRaw
	s.previousRawReady = true
	s.firstSmoothing.append(firstRaw)
	if !s.firstSmoothing.ready {
		return
	}

	s.firstWindow.append(s.firstSmoothing.value)
	low, high, ok = s.firstWindow.bounds()
	if !ok {
		return
	}
	secondRaw := stochasticValue(s.firstSmoothing.value, low, high, s.previousCycle, s.previousCycleReady)
	s.previousCycle = secondRaw
	s.previousCycleReady = true
	s.secondSmoothing.append(secondRaw)
	if !s.secondSmoothing.ready {
		return
	}

	if s.pointCount > 0 {
		s.previous = s.current
	}
	s.current = clampSTC(s.secondSmoothing.value)
	s.pointCount++
}

func stochasticValue(value float64, low float64, high float64, fallback float64, fallbackReady bool) float64 {
	if high-low <= 1e-12 {
		if fallbackReady {
			return fallback
		}
		return 0
	}
	return clampSTC(100 * (value - low) / (high - low))
}

func clampSTC(value float64) float64 {
	return math.Max(0, math.Min(100, value))
}

func (s *basicIndicatorState) stcValue() (float64, float64, bool) {
	if s == nil || s.stc.pointCount == 0 {
		return 0, 0, false
	}
	return s.stc.current, s.stc.previous, true
}

func stcValue(closes []float64) (float64, float64, bool) {
	state := newStreamSTCState()
	for _, closeValue := range closes {
		state.append(closeValue)
	}
	if state.pointCount == 0 {
		return 0, 0, false
	}
	return state.current, state.previous, true
}

func addSTCFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, current float64, previous float64) {
	setValueTarget(target, values, "stc", current, true)
	setValueTarget(target, values, "stc_delta", current-previous, true)
	signals["stc_direction"] = stcDirection(current, previous)
	signals["stc_zone"] = stcZone(current)
	signals["stc_cross"] = stcCross(current, previous)
}

func stcDirection(current float64, previous float64) string {
	switch {
	case current > previous+1e-9:
		return "rising"
	case current < previous-1e-9:
		return "falling"
	default:
		return "flat"
	}
}

func stcZone(value float64) string {
	switch {
	case value <= stcOversold:
		return "oversold"
	case value >= stcOverbought:
		return "overbought"
	default:
		return "neutral"
	}
}

func stcCross(current float64, previous float64) string {
	switch {
	case previous <= stcOversold && current > stcOversold:
		return "up_25"
	case previous >= stcOversold && current < stcOversold:
		return "down_25"
	case previous <= stcOverbought && current > stcOverbought:
		return "up_75"
	case previous >= stcOverbought && current < stcOverbought:
		return "down_75"
	default:
		return "none"
	}
}

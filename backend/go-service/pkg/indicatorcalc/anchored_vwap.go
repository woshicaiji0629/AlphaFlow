package indicatorcalc

import "math"

const (
	dynamicSwingVWAPPeriod      = 50
	dynamicSwingVWAPBaseAPT     = 20.0
	dynamicSwingVWAPUseAdapt    = false
	dynamicSwingVWAPVolBias     = 10.0
	dynamicSwingVWAPATRPeriod   = 50
	dynamicSwingVWAPNearPercent = 0.2
)

type dynamicSwingVWAPState struct {
	value      float64
	anchor     float64
	anchorAge  int
	dir        int
	anchorType string
	swingLabel string
	ok         bool
}

func addDynamicSwingAnchoredVWAP(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64) {
	addDynamicSwingAnchoredVWAPToSet(nil, values, signals, highs, lows, closes, volumes, nil)
}

func addDynamicSwingAnchoredVWAPToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64, basic *basicIndicatorState) {
	state, ok := basic.dynamicSwingVWAPValue(dynamicSwingVWAPPeriod, dynamicSwingVWAPBaseAPT, dynamicSwingVWAPUseAdapt, dynamicSwingVWAPVolBias)
	if !ok {
		state = dynamicSwingAnchoredVWAP(highs, lows, closes, volumes, dynamicSwingVWAPPeriod, dynamicSwingVWAPBaseAPT, dynamicSwingVWAPUseAdapt, dynamicSwingVWAPVolBias)
	}
	if !state.ok {
		return
	}
	last := closes[len(closes)-1]
	setValueTarget(target, values, "dynamic_swing_vwap", state.value, true)
	setValueTarget(target, values, "dynamic_swing_vwap_distance_pct", percentDistance(last, state.value), state.value != 0)
	setValueTarget(target, values, "dynamic_swing_vwap_anchor_price", state.anchor, true)
	setValueTarget(target, values, "dynamic_swing_vwap_anchor_age", float64(state.anchorAge), true)
	signals["dynamic_swing_vwap_direction"] = dynamicSwingVWAPDirection(state.dir)
	signals["dynamic_swing_vwap_position"] = dynamicSwingVWAPPosition(last, state.value)
	signals["dynamic_swing_vwap_anchor_type"] = state.anchorType
	signals["dynamic_swing_vwap_swing_label"] = state.swingLabel
}

type streamDynamicSwingVWAPState struct {
	period      int
	baseAPT     float64
	fixedAlpha  float64
	highWindow  floatMonotonicWindow
	lowWindow   floatMonotonicWindow
	index       int
	ph          float64
	pl          float64
	phIndex     int
	plIndex     int
	previousDir int
	prevSwing   float64
	p           float64
	volume      float64
	state       dynamicSwingVWAPState
}

func newStreamDynamicSwingVWAPState(period int, baseAPT float64) streamDynamicSwingVWAPState {
	return streamDynamicSwingVWAPState{
		period: period, baseAPT: baseAPT, fixedAlpha: alphaFromAPT(baseAPT),
		highWindow: newFloatMonotonicWindow(true), lowWindow: newFloatMonotonicWindow(false),
		index: -1, ph: math.NaN(), pl: math.NaN(), prevSwing: math.NaN(),
	}
}

func (s *streamDynamicSwingVWAPState) append(highs []float64, lows []float64, closes []float64, volumes []float64) {
	if s == nil || s.period < 2 || len(closes) == 0 || len(highs) != len(closes) || len(lows) != len(closes) || len(volumes) != len(closes) {
		return
	}
	last := len(closes) - 1
	s.index++
	index := s.index
	prevPh, prevPl := s.ph, s.pl
	s.highWindow.push(index, highs[last])
	s.lowWindow.push(index, lows[last])
	s.highWindow.expireBefore(index - s.period + 1)
	s.lowWindow.expireBefore(index - s.period + 1)
	windowHigh, highOK := s.highWindow.value()
	windowLow, lowOK := s.lowWindow.value()
	if highOK && highs[last] == windowHigh {
		s.ph, s.phIndex = highs[last], index
	}
	if lowOK && lows[last] == windowLow {
		s.pl, s.plIndex = lows[last], index
	}

	dir := -1
	if s.phIndex > s.plIndex {
		dir = 1
	}
	if index == 0 {
		s.previousDir = dir
		typical := (highs[last] + lows[last] + closes[last]) / 3
		s.p = typical * volumes[last]
		s.volume = volumes[last]
	}

	if dir != s.previousDir {
		anchorIndex, anchorPrice, anchorType := s.phIndex, s.ph, "swing_high"
		if dir > 0 {
			anchorIndex, anchorPrice, anchorType = s.plIndex, s.pl, "swing_low"
		}
		anchorOffset := index - anchorIndex
		anchorLocal := last - anchorOffset
		if anchorLocal < 0 || anchorLocal > last || volumes[anchorLocal] <= 0 {
			s.previousDir = dir
			return
		}
		s.p = anchorPrice * volumes[anchorLocal]
		s.volume = volumes[anchorLocal]
		s.state.swingLabel = dynamicSwingLabel(dir, s.ph, s.pl, prevPh, prevPl, s.prevSwing)
		if dir > 0 {
			s.prevSwing = prevPh
		} else {
			s.prevSwing = prevPl
		}
		s.state.anchor = anchorPrice
		s.state.anchorAge = anchorOffset
		s.state.anchorType = anchorType
		for cursor := anchorLocal; cursor <= last; cursor++ {
			s.p, s.volume, s.state.value = dynamicSwingVWAPStepAlpha(s.p, s.volume, highs[cursor], lows[cursor], closes[cursor], volumes[cursor], s.fixedAlpha)
		}
	} else {
		initializedAnchor := false
		if s.state.anchorType == "" {
			s.state.anchor, s.state.anchorAge, s.state.anchorType = dynamicSwingCurrentAnchor(dir, s.ph, s.pl, s.phIndex, s.plIndex, index)
			s.state.swingLabel = "none"
			initializedAnchor = true
		}
		s.p, s.volume, s.state.value = dynamicSwingVWAPStepAlpha(s.p, s.volume, highs[last], lows[last], closes[last], volumes[last], s.fixedAlpha)
		if !initializedAnchor {
			s.state.anchorAge++
		}
	}
	s.state.dir = dir
	s.state.ok = index+1 >= s.period && s.volume > 0 && !math.IsNaN(s.state.value)
	s.previousDir = dir
}

func (s *streamDynamicSwingVWAPState) value(period int, baseAPT float64, useAdapt bool, volBias float64) (dynamicSwingVWAPState, bool) {
	if s == nil || period != s.period || baseAPT != s.baseAPT || useAdapt || volBias != dynamicSwingVWAPVolBias || !s.state.ok {
		return dynamicSwingVWAPState{}, false
	}
	return s.state, true
}

func dynamicSwingAnchoredVWAP(highs []float64, lows []float64, closes []float64, volumes []float64, period int, baseAPT float64, useAdapt bool, volBias float64) dynamicSwingVWAPState {
	if period < 2 || baseAPT < 1 || len(closes) < period || len(highs) != len(closes) || len(lows) != len(closes) || len(volumes) != len(closes) {
		return dynamicSwingVWAPState{}
	}

	var aptSeries []float64
	if useAdapt {
		aptSeries = dynamicSwingAPTSeries(highs, lows, closes, baseAPT, true, volBias)
	}
	fixedAlpha := alphaFromAPT(baseAPT)
	ph := math.NaN()
	pl := math.NaN()
	phL := 0
	plL := 0
	prevSwing := math.NaN()
	prevDir := 0
	p := ((highs[0] + lows[0] + closes[0]) / 3) * volumes[0]
	vol := volumes[0]
	state := dynamicSwingVWAPState{}
	highWindow := newFloatMonotonicWindow(true)
	lowWindow := newFloatMonotonicWindow(false)

	for index := range closes {
		prevPh := ph
		prevPl := pl
		highWindow.push(index, highs[index])
		lowWindow.push(index, lows[index])
		oldestIndex := index - period + 1
		highWindow.expireBefore(oldestIndex)
		lowWindow.expireBefore(oldestIndex)
		windowHigh, highOK := highWindow.value()
		windowLow, lowOK := lowWindow.value()
		if highOK && highs[index] == windowHigh {
			ph = highs[index]
			phL = index
		}
		if lowOK && lows[index] == windowLow {
			pl = lows[index]
			plL = index
		}

		dir := -1
		if phL > plL {
			dir = 1
		}
		if index == 0 {
			prevDir = dir
		}

		if dir != prevDir {
			anchorIndex := phL
			anchorPrice := ph
			anchorType := "swing_high"
			if dir > 0 {
				anchorIndex = plL
				anchorPrice = pl
				anchorType = "swing_low"
			}
			if anchorIndex < 0 || anchorIndex > index || volumes[anchorIndex] <= 0 {
				prevDir = dir
				continue
			}
			p = anchorPrice * volumes[anchorIndex]
			vol = volumes[anchorIndex]
			state.swingLabel = dynamicSwingLabel(dir, ph, pl, prevPh, prevPl, prevSwing)
			if dir > 0 {
				prevSwing = prevPh
			} else {
				prevSwing = prevPl
			}
			state.anchor = anchorPrice
			state.anchorAge = index - anchorIndex
			state.anchorType = anchorType
			for cursor := anchorIndex; cursor <= index; cursor++ {
				alpha := fixedAlpha
				if useAdapt {
					alpha = alphaFromAPT(dynamicSwingAPTAt(aptSeries, baseAPT, cursor))
				}
				p, vol, state.value = dynamicSwingVWAPStepAlpha(p, vol, highs[cursor], lows[cursor], closes[cursor], volumes[cursor], alpha)
			}
		} else {
			initializedAnchor := false
			if state.anchorType == "" {
				state.anchor, state.anchorAge, state.anchorType = dynamicSwingCurrentAnchor(dir, ph, pl, phL, plL, index)
				state.swingLabel = "none"
				initializedAnchor = true
			}
			alpha := fixedAlpha
			if useAdapt {
				alpha = alphaFromAPT(dynamicSwingAPTAt(aptSeries, baseAPT, index))
			}
			p, vol, state.value = dynamicSwingVWAPStepAlpha(p, vol, highs[index], lows[index], closes[index], volumes[index], alpha)
			if !initializedAnchor {
				state.anchorAge++
			}
		}
		state.dir = dir
		state.ok = vol > 0 && !math.IsNaN(state.value)
		prevDir = dir
	}

	return state
}

func dynamicSwingAPTAt(series []float64, baseAPT float64, index int) float64 {
	if index >= 0 && index < len(series) {
		return series[index]
	}
	return baseAPT
}

func dynamicSwingCurrentAnchor(dir int, ph float64, pl float64, phL int, plL int, index int) (float64, int, string) {
	if dir > 0 {
		return pl, index - plL, "swing_low"
	}
	return ph, index - phL, "swing_high"
}

func dynamicSwingVWAPStep(previousP float64, previousVol float64, high float64, low float64, close float64, volume float64, apt float64) (float64, float64, float64) {
	return dynamicSwingVWAPStepAlpha(previousP, previousVol, high, low, close, volume, alphaFromAPT(apt))
}

func dynamicSwingVWAPStepAlpha(previousP float64, previousVol float64, high float64, low float64, close float64, volume float64, alpha float64) (float64, float64, float64) {
	pxv := ((high + low + close) / 3) * volume
	p := (1-alpha)*previousP + alpha*pxv
	vol := (1-alpha)*previousVol + alpha*volume
	if vol <= 0 {
		return p, vol, math.NaN()
	}
	return p, vol, p / vol
}

func dynamicSwingAPTSeries(highs []float64, lows []float64, closes []float64, baseAPT float64, useAdapt bool, volBias float64) []float64 {
	values := make([]float64, len(closes))
	for index := range values {
		values[index] = baseAPT
	}
	if !useAdapt {
		return values
	}
	atrValues, ok := atrSeries(highs, lows, closes, dynamicSwingVWAPATRPeriod)
	if !ok {
		return values
	}
	for index := dynamicSwingVWAPATRPeriod; index < len(closes); index++ {
		atrIndex := index - dynamicSwingVWAPATRPeriod
		atrAvg, avgOK := sma(atrValues[:atrIndex+1], minInt(dynamicSwingVWAPATRPeriod, atrIndex+1))
		if !avgOK || atrAvg <= 0 {
			continue
		}
		ratio := atrValues[atrIndex] / atrAvg
		aptRaw := baseAPT / math.Pow(ratio, volBias)
		values[index] = math.Round(math.Max(5, math.Min(300, aptRaw)))
	}
	return values
}

func alphaFromAPT(apt float64) float64 {
	decay := math.Exp(-math.Log(2) / math.Max(1, apt))
	return 1 - decay
}

func isHighestAt(values []float64, period int, index int) bool {
	start := index - period + 1
	if start < 0 {
		start = 0
	}
	for cursor := start; cursor <= index; cursor++ {
		if values[cursor] > values[index] {
			return false
		}
	}
	return true
}

func isLowestAt(values []float64, period int, index int) bool {
	start := index - period + 1
	if start < 0 {
		start = 0
	}
	for cursor := start; cursor <= index; cursor++ {
		if values[cursor] < values[index] {
			return false
		}
	}
	return true
}

func dynamicSwingLabel(dir int, ph float64, pl float64, prevPh float64, prevPl float64, previous float64) string {
	if math.IsNaN(previous) {
		return "none"
	}
	if dir > 0 {
		switch {
		case pl < previous:
			return "LL"
		case pl > previous:
			return "HL"
		default:
			return "none"
		}
	}
	switch {
	case ph < previous:
		return "LH"
	case ph > previous:
		return "HH"
	case !math.IsNaN(prevPh) && !math.IsNaN(prevPl):
		return "none"
	default:
		return "none"
	}
}

func dynamicSwingVWAPDirection(dir int) string {
	if dir > 0 {
		return "bull"
	}
	return "bear"
}

func dynamicSwingVWAPPosition(close float64, vwap float64) string {
	distance := percentDistance(close, vwap)
	switch {
	case math.Abs(distance) <= dynamicSwingVWAPNearPercent:
		return "near"
	case distance > 0:
		return "above"
	default:
		return "below"
	}
}

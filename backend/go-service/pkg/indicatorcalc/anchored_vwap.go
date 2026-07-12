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
	addDynamicSwingAnchoredVWAPToSet(nil, values, signals, highs, lows, closes, volumes)
}

func addDynamicSwingAnchoredVWAPToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64) {
	state := dynamicSwingAnchoredVWAP(highs, lows, closes, volumes, dynamicSwingVWAPPeriod, dynamicSwingVWAPBaseAPT, dynamicSwingVWAPUseAdapt, dynamicSwingVWAPVolBias)
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

	for index := range closes {
		prevPh := ph
		prevPl := pl
		if isHighestAt(highs, period, index) {
			ph = highs[index]
			phL = index
		}
		if isLowestAt(lows, period, index) {
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

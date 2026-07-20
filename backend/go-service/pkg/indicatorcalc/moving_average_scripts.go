package indicatorcalc

import "math"

func addScriptDualMovingAverageToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, volumes []float64) {
	const (
		period1  = 20
		period2  = 50
		maType1  = 1
		maType2  = 1
		smooth   = 2
		t3Factor = 0.7
	)
	if len(closes) < period2+smooth+1 {
		return
	}
	out1, ok1 := movingAverageByType(closes, volumes, period1, maType1, t3Factor)
	out2, ok2 := movingAverageByType(closes, volumes, period2, maType2, t3Factor)
	prevOut1, okPrev1 := movingAverageByType(closes[:len(closes)-1], volumes[:len(volumes)-1], period1, maType1, t3Factor)
	prevOut2, okPrev2 := movingAverageByType(closes[:len(closes)-1], volumes[:len(volumes)-1], period2, maType2, t3Factor)
	smoothOut1, okSmooth := movingAverageByType(closes[:len(closes)-smooth], volumes[:len(volumes)-smooth], period1, maType1, t3Factor)
	if !ok1 || !ok2 || !okPrev1 || !okPrev2 || !okSmooth {
		return
	}
	setValueTarget(target, values, "script_dual_ma_out1", out1, true)
	setValueTarget(target, values, "script_dual_ma_out2", out2, true)
	setValueTarget(target, values, "script_dual_ma_out1_slope_pct", percentDistance(out1, smoothOut1), smoothOut1 != 0)
	setValueTarget(target, values, "script_dual_ma_out2_slope_pct", percentDistance(out2, prevOut2), prevOut2 != 0)
	signals["script_ma1_direction"] = maDirection(out1, smoothOut1)
	signals["script_price_cross_ma1"] = priceCrossMA(closes, out1)
	signals["script_price_cross_ma2"] = priceCrossMA(closes, out2)
	signals["script_dual_ma_cross"] = crossSignal(prevOut1, prevOut2, out1, out2)
}

func maDirection(current float64, previous float64) string {
	switch {
	case current > previous:
		return "up"
	case current < previous:
		return "down"
	default:
		return "flat"
	}
}

func priceCrossMA(closes []float64, average float64) string {
	last := len(closes) - 1
	openLike := closes[last-1]
	closeValue := closes[last]
	switch {
	case openLike < average && closeValue > average:
		return "up"
	case openLike > average && closeValue < average:
		return "down"
	default:
		return "none"
	}
}

func addScriptMovingAverageSignalToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, basic *basicIndicatorState) {
	if len(closes) < 28 {
		return
	}
	ema10, ok10 := emaFromStateOrSeries(basic, closes, 10)
	breakthrough, okBreakthrough := previousEMAFromStateOrSeries(basic, closes, 13, 1)
	ema12, ok12 := emaFromStateOrSeries(basic, closes, 12)
	ema26, ok26 := emaFromStateOrSeries(basic, closes, 26)
	prevEMA10, okPrev10 := previousEMAFromStateOrSeries(basic, closes, 10, 1)
	prevBreakthrough, okPrevBreakthrough := ema(closes[:len(closes)-2], 13)
	if !ok10 || !okBreakthrough || !ok12 || !ok26 || !okPrev10 || !okPrevBreakthrough || breakthrough == 0 || prevBreakthrough == 0 {
		return
	}
	a1x := (ema10 - breakthrough) / breakthrough * 100
	prevA1x := (prevEMA10 - prevBreakthrough) / prevBreakthrough * 100
	midDirection := ema12 - ema26
	setValueTarget(target, values, "script_ma_breakout_pct", a1x, true)
	setValueTarget(target, values, "script_ma_mid_direction", midDirection, true)
	switch {
	case prevA1x <= 0 && a1x > 0 && midDirection > 0:
		signals["script_ma_signal"] = "bull_breakout"
	case prevA1x >= 0 && a1x < 0 && midDirection < 0:
		signals["script_ma_signal"] = "bear_breakout"
	case a1x >= 0:
		signals["script_ma_signal"] = "bull_color"
	default:
		signals["script_ma_signal"] = "bear_color"
	}
}

func addEMDFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, period int, multiplier float64) {
	avg, previousAvg, emd, previousEMD, ok := emdLastTwo(closes, period)
	if !ok {
		return
	}
	upper := avg + emd*multiplier
	lower := avg - emd*multiplier
	previousUpper := previousAvg + previousEMD*multiplier
	previousLower := previousAvg - previousEMD*multiplier
	current := closes[len(closes)-1]
	previous := closes[len(closes)-2]

	setValueTarget(target, values, "emd_avg", avg, true)
	setValueTarget(target, values, "emd_value", emd, true)
	setValueTarget(target, values, "emd_upper", upper, true)
	setValueTarget(target, values, "emd_lower", lower, true)
	switch {
	case current > upper:
		signals["emd_direction"] = "up"
	case current < lower:
		signals["emd_direction"] = "down"
	default:
		signals["emd_direction"] = "range"
	}
	switch {
	case previous <= previousUpper && current > upper:
		signals["emd_cross"] = "golden"
	case previous >= previousLower && current < lower:
		signals["emd_cross"] = "dead"
	default:
		signals["emd_cross"] = "none"
	}
}

func emdLastTwo(closes []float64, period int) (float64, float64, float64, float64, bool) {
	if period <= 0 || len(closes)-period+1 < period+2 {
		return 0, 0, 0, 0, false
	}
	avg, _ := sma(closes[:period], period)
	previousAvg := avg
	deviationSum := 0.0
	emd := 0.0
	previousEMD := 0.0
	emaMultiplier := 2 / float64(period+1)
	deviationIndex := 0
	for closeIndex := period - 1; closeIndex < len(closes); closeIndex++ {
		if closeIndex >= period {
			previousAvg = avg
			avg = (avg*float64(period-1) + closes[closeIndex]) / float64(period)
		}
		deviation := math.Abs(closes[closeIndex] - avg)
		if deviationIndex < period {
			deviationSum += deviation
			if deviationIndex == period-1 {
				emd = deviationSum / float64(period)
			}
		} else {
			previousEMD = emd
			emd = (deviation-emd)*emaMultiplier + emd
		}
		deviationIndex++
	}
	return avg, previousAvg, emd, previousEMD, true
}

func alligatorDirection(jaw float64, teeth float64, lips float64) string {
	switch {
	case lips > teeth && teeth > jaw:
		return "bull"
	case lips < teeth && teeth < jaw:
		return "bear"
	default:
		return "mixed"
	}
}

func alligatorState(spreadPct float64) string {
	switch {
	case spreadPct >= 0.8:
		return "eating"
	case spreadPct >= 0.25:
		return "awakening"
	default:
		return "sleeping"
	}
}

package indicator

func addVolatilityCoreFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int) {
	atrValue, ok := atr(highs, lows, closes, period)
	setValue(values, "atr14", atrValue, ok)
	if ok {
		last := closes[len(closes)-1]
		setValue(values, "atr_pct14", atrValue/last*100, last != 0)
		setValue(values, "natr14", atrValue/last*100, last != 0)
		signals["volatility_state"] = volatilityState(highs, lows, closes, period)
	}
	adxValue, plusDI, minusDI, ok := adx(highs, lows, closes, period)
	if ok {
		setValue(values, "adx14", adxValue, true)
		setValue(values, "di_plus14", plusDI, true)
		setValue(values, "di_minus14", minusDI, true)
		signals["adx_trend_strength"] = adxTrendStrength(adxValue)
		signals["di_direction"] = diDirection(plusDI, minusDI)
	}
}

func atr(highs []float64, lows []float64, closes []float64, period int) (float64, bool) {
	series, ok := atrSeries(highs, lows, closes, period)
	if !ok {
		return 0, false
	}
	return series[len(series)-1], true
}

func atrSeries(highs []float64, lows []float64, closes []float64, period int) ([]float64, bool) {
	if period <= 0 || len(closes) <= period {
		return nil, false
	}
	trs := trueRanges(highs, lows, closes)
	if len(trs) < period {
		return nil, false
	}
	values := make([]float64, 0, len(trs)-period+1)
	first, _ := sma(trs[:period], period)
	values = append(values, first)
	current := first
	for index := period; index < len(trs); index++ {
		current = (current*float64(period-1) + trs[index]) / float64(period)
		values = append(values, current)
	}
	return values, true
}

func adx(highs []float64, lows []float64, closes []float64, period int) (float64, float64, float64, bool) {
	if period <= 0 || len(closes) <= period {
		return 0, 0, 0, false
	}
	trs := trueRanges(highs, lows, closes)
	if len(trs) < period || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, 0, 0, false
	}
	dxValues := make([]float64, 0, len(closes)-1)
	var smoothedTR float64
	var smoothedPlusDM float64
	var smoothedMinusDM float64
	var plusDI float64
	var minusDI float64
	for index := 1; index < len(closes); index++ {
		trueRange := maxFloat(
			highs[index]-lows[index],
			absFloat(highs[index]-closes[index-1]),
			absFloat(lows[index]-closes[index-1]),
		)
		upMove := directionalMovementPlus(highs[index], highs[index-1], lows[index], lows[index-1])
		downMove := directionalMovementMinus(highs[index], highs[index-1], lows[index], lows[index-1])
		smoothedTR = smoothedTR - smoothedTR/float64(period) + trueRange
		smoothedPlusDM = smoothedPlusDM - smoothedPlusDM/float64(period) + upMove
		smoothedMinusDM = smoothedMinusDM - smoothedMinusDM/float64(period) + downMove
		var dx float64
		plusDI, minusDI, dx = directionalIndex(smoothedTR, smoothedPlusDM, smoothedMinusDM)
		dxValues = append(dxValues, dx)
	}
	if len(dxValues) < period {
		return 0, 0, 0, false
	}
	adxValue, _ := sma(dxValues, period)
	return adxValue, plusDI, minusDI, true
}

func directionalMovementPlus(currentHigh float64, previousHigh float64, currentLow float64, previousLow float64) float64 {
	upMove := currentHigh - previousHigh
	downMove := previousLow - currentLow
	if upMove > downMove && upMove > 0 {
		return upMove
	}
	return 0
}

func directionalMovementMinus(currentHigh float64, previousHigh float64, currentLow float64, previousLow float64) float64 {
	upMove := currentHigh - previousHigh
	downMove := previousLow - currentLow
	if downMove > upMove && downMove > 0 {
		return downMove
	}
	return 0
}

func directionalIndex(smoothedTR float64, smoothedPlusDM float64, smoothedMinusDM float64) (float64, float64, float64) {
	if smoothedTR == 0 {
		return 0, 0, 0
	}
	plusDI := 100 * smoothedPlusDM / smoothedTR
	minusDI := 100 * smoothedMinusDM / smoothedTR
	if plusDI+minusDI == 0 {
		return plusDI, minusDI, 0
	}
	dx := abs(plusDI-minusDI) / (plusDI + minusDI) * 100
	return plusDI, minusDI, dx
}

func volatilityState(highs []float64, lows []float64, closes []float64, period int) string {
	series, ok := atrSeries(highs, lows, closes, period)
	if !ok || len(series) < 6 {
		return "normal"
	}
	last := len(series) - 1
	recent := series[last]
	previous := series[last-5]
	switch {
	case previous != 0 && recent > previous*1.1:
		return "expanding"
	case previous != 0 && recent < previous*0.9:
		return "contracting"
	default:
		return "normal"
	}
}

func adxTrendStrength(value float64) string {
	switch {
	case value >= 25:
		return "strong"
	case value >= 15:
		return "weak"
	default:
		return "none"
	}
}

func diDirection(plusDI float64, minusDI float64) string {
	switch {
	case plusDI > minusDI:
		return "bull"
	case minusDI > plusDI:
		return "bear"
	default:
		return "neutral"
	}
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

package indicator

import "math"

func addMovingAverageFeatures(values map[string]string, signals map[string]string, closes []float64, volumes []float64) {
	hma21, ok := hma(closes, 21)
	setValue(values, "hma21", hma21, ok)
	vwma20, ok := vwma(closes, volumes, 20)
	setValue(values, "vwma20", vwma20, ok)
	dema21, ok := dema(closes, 21)
	setValue(values, "dema21", dema21, ok)
	tema21, ok := tema(closes, 21)
	setValue(values, "tema21", tema21, ok)
	kama10, ok := kama(closes, 10, 2, 30)
	setValue(values, "kama10", kama10, ok)

	if len(closes) >= 30 {
		recentHMA, okRecent := hma(closes, 21)
		previousHMA, okPrevious := hma(closes[:len(closes)-3], 21)
		if okRecent && okPrevious && previousHMA != 0 {
			setValue(values, "hma21_slope3_pct", percentDistance(recentHMA, previousHMA), true)
		}
	}

	ema7, ok7 := ema(closes, 7)
	ema25, ok25 := ema(closes, 25)
	ema99, ok99 := ema(closes, 99)
	last := closes[len(closes)-1]
	if ok7 && ok25 && ok99 {
		spread := (ema7 - ema99) / last * 100
		setValue(values, "ema_spread_pct", spread, last != 0)
		signals["ma_state"] = movingAverageState(ema7, ema25, ema99, last)
		setValue(values, "ma_trend_strength", math.Abs(spread), true)
	}
}

func hma(values []float64, period int) (float64, bool) {
	if period <= 1 || len(values) < period {
		return 0, false
	}
	half := period / 2
	sqrtPeriod := int(math.Sqrt(float64(period)))
	if sqrtPeriod < 1 {
		return 0, false
	}
	differences := make([]float64, 0, len(values)-period+1)
	for end := period; end <= len(values); end++ {
		halfWMA, okHalf := wma(values[end-half:end], half)
		fullWMA, okFull := wma(values[end-period:end], period)
		if !okHalf || !okFull {
			return 0, false
		}
		differences = append(differences, 2*halfWMA-fullWMA)
	}
	return wma(differences, sqrtPeriod)
}

func vwma(values []float64, volumes []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period || len(volumes) != len(values) {
		return 0, false
	}
	start := len(values) - period
	var weighted float64
	var volumeSum float64
	for index := start; index < len(values); index++ {
		weighted += values[index] * volumes[index]
		volumeSum += volumes[index]
	}
	if volumeSum == 0 {
		return 0, false
	}
	return weighted / volumeSum, true
}

func dema(values []float64, period int) (float64, bool) {
	ema1, ok := emaSeries(values, period)
	if !ok {
		return 0, false
	}
	ema2, ok := emaSeries(ema1, period)
	if !ok {
		return 0, false
	}
	return 2*ema1[len(ema1)-1] - ema2[len(ema2)-1], true
}

func tema(values []float64, period int) (float64, bool) {
	ema1, ok := emaSeries(values, period)
	if !ok {
		return 0, false
	}
	ema2, ok := emaSeries(ema1, period)
	if !ok {
		return 0, false
	}
	ema3, ok := emaSeries(ema2, period)
	if !ok {
		return 0, false
	}
	return 3*ema1[len(ema1)-1] - 3*ema2[len(ema2)-1] + ema3[len(ema3)-1], true
}

func kama(values []float64, period int, fast int, slow int) (float64, bool) {
	if period <= 0 || fast <= 0 || slow <= 0 || len(values) <= period {
		return 0, false
	}
	fastSC := 2.0 / float64(fast+1)
	slowSC := 2.0 / float64(slow+1)
	current := values[period]
	for index := period + 1; index < len(values); index++ {
		change := math.Abs(values[index] - values[index-period])
		var volatility float64
		for offset := index - period + 1; offset <= index; offset++ {
			volatility += math.Abs(values[offset] - values[offset-1])
		}
		efficiency := 0.0
		if volatility != 0 {
			efficiency = change / volatility
		}
		smoothing := math.Pow(efficiency*(fastSC-slowSC)+slowSC, 2)
		current = current + smoothing*(values[index]-current)
	}
	return current, true
}

func movingAverageState(ema7 float64, ema25 float64, ema99 float64, last float64) string {
	switch {
	case last > ema7 && ema7 > ema25 && ema25 > ema99:
		return "bull"
	case last < ema7 && ema7 < ema25 && ema25 < ema99:
		return "bear"
	default:
		return "mixed"
	}
}

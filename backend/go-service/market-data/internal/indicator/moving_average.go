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
	addAlligatorFeatures(values, signals, closes)

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
		signals["ma_arrangement"] = movingAverageArrangement(ema7, ema25, ema99)
		setValue(values, "ma_trend_strength", math.Abs(spread), true)
		addMovingAverageStructureFeatures(values, signals, closes, ema7, ema25, ema99)
	}
}

func addAlligatorFeatures(values map[string]string, signals map[string]string, closes []float64) {
	jaw, teeth, lips, ok := alligator(closes)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValue(values, "alligator_jaw", jaw, true)
	setValue(values, "alligator_teeth", teeth, true)
	setValue(values, "alligator_lips", lips, true)
	spread := (maxFloat(jaw, teeth, lips) - minFloat(jaw, teeth, lips)) / last * 100
	setValue(values, "alligator_spread_pct", spread, last != 0)
	signals["alligator_direction"] = alligatorDirection(jaw, teeth, lips)
	signals["alligator_state"] = alligatorState(spread)
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

func alligator(values []float64) (float64, float64, float64, bool) {
	jaw, okJaw := smma(values, 13)
	teeth, okTeeth := smma(values, 8)
	lips, okLips := smma(values, 5)
	if !okJaw || !okTeeth || !okLips {
		return 0, 0, 0, false
	}
	return jaw, teeth, lips, true
}

func smma(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period {
		return 0, false
	}
	current, _ := sma(values[:period], period)
	for index := period; index < len(values); index++ {
		current = (current*float64(period-1) + values[index]) / float64(period)
	}
	return current, true
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

func addMovingAverageStructureFeatures(values map[string]string, signals map[string]string, closes []float64, ema7 float64, ema25 float64, ema99 float64) {
	if len(closes) < 110 {
		return
	}
	prevEMA7, ok7 := ema(closes[:len(closes)-1], 7)
	prevEMA25, ok25 := ema(closes[:len(closes)-1], 25)
	prevEMA99, ok99 := ema(closes[:len(closes)-5], 99)
	if ok7 && ok25 {
		signals["ma_cross"] = crossSignal(prevEMA7, prevEMA25, ema7, ema25)
	}
	if ok7 && ok25 && ok99 {
		currentSpread := maxFloat(ema7, ema25, ema99) - minFloat(ema7, ema25, ema99)
		previousSpread := maxFloat(prevEMA7, prevEMA25, prevEMA99) - minFloat(prevEMA7, prevEMA25, prevEMA99)
		setValue(values, "ma_group_spread_pct", currentSpread/closes[len(closes)-1]*100, closes[len(closes)-1] != 0)
		signals["ma_spread_state"] = spreadState(currentSpread, previousSpread)
		signals["ma_compression"] = compressionState(currentSpread, closes[len(closes)-1])
	}
	prevEMA25, okPrevSlope := ema(closes[:len(closes)-5], 25)
	if okPrevSlope && prevEMA25 != 0 {
		slopePct := percentDistance(ema25, prevEMA25)
		signals["ma_slope_state"] = slopeState(slopePct)
	}
	signals["ma_breakout"] = movingAverageBreakout(closes[len(closes)-1], ema7, ema25, ema99)
}

func movingAverageArrangement(ema7 float64, ema25 float64, ema99 float64) string {
	switch {
	case ema7 > ema25 && ema25 > ema99:
		return "bull"
	case ema7 < ema25 && ema25 < ema99:
		return "bear"
	default:
		return "mixed"
	}
}

func spreadState(current float64, previous float64) string {
	threshold := 0.00000001
	if previous > 0 {
		threshold = previous * 0.08
	}
	switch {
	case current > previous+threshold:
		return "expanding"
	case current < previous-threshold:
		return "contracting"
	default:
		return "flat"
	}
}

func compressionState(spread float64, price float64) string {
	if price == 0 {
		return "normal"
	}
	if spread/price*100 <= 0.25 {
		return "compressed"
	}
	return "normal"
}

func slopeState(slopePct float64) string {
	switch {
	case slopePct > 0.08:
		return "up"
	case slopePct < -0.08:
		return "down"
	default:
		return "flat"
	}
}

func movingAverageBreakout(price float64, ema7 float64, ema25 float64, ema99 float64) string {
	upper := maxFloat(ema7, ema25, ema99)
	lower := minFloat(ema7, ema25, ema99)
	switch {
	case price > upper:
		return "above_group"
	case price < lower:
		return "below_group"
	default:
		return "inside_group"
	}
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

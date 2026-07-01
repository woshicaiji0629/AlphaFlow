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
	addScriptDualMovingAverage(values, signals, closes, volumes)
	addScriptMovingAverageSignal(values, signals, closes)
	addEMDFeatures(values, signals, closes, 25, 1)
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

func tilsonT3(values []float64, period int, factor float64) (float64, bool) {
	first, ok := gd(values, period, factor)
	if !ok {
		return 0, false
	}
	second, ok := gd(first, period, factor)
	if !ok {
		return 0, false
	}
	third, ok := gd(second, period, factor)
	if !ok {
		return 0, false
	}
	return third[len(third)-1], true
}

func gd(values []float64, period int, factor float64) ([]float64, bool) {
	first, ok := emaSeries(values, period)
	if !ok {
		return nil, false
	}
	second, ok := emaSeries(first, period)
	if !ok {
		return nil, false
	}
	offset := len(first) - len(second)
	result := make([]float64, 0, len(second))
	for index, secondValue := range second {
		result = append(result, first[index+offset]*(1+factor)-secondValue*factor)
	}
	return result, true
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

func smmaSeries(values []float64, period int) ([]float64, bool) {
	if period <= 0 || len(values) < period {
		return nil, false
	}
	result := make([]float64, 0, len(values)-period+1)
	current, _ := sma(values[:period], period)
	result = append(result, current)
	for index := period; index < len(values); index++ {
		current = (current*float64(period-1) + values[index]) / float64(period)
		result = append(result, current)
	}
	return result, true
}

func movingAverageByType(values []float64, volumes []float64, period int, maType int, t3Factor float64) (float64, bool) {
	switch maType {
	case 1:
		return sma(values, period)
	case 2:
		return ema(values, period)
	case 3:
		return wma(values, period)
	case 4:
		return hma(values, period)
	case 5:
		return vwma(values, volumes, period)
	case 6:
		return smma(values, period)
	case 7:
		return tema(values, period)
	default:
		return tilsonT3(values, period, t3Factor)
	}
}

func addScriptDualMovingAverage(values map[string]string, signals map[string]string, closes []float64, volumes []float64) {
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
	setValue(values, "script_dual_ma_out1", out1, true)
	setValue(values, "script_dual_ma_out2", out2, true)
	setValue(values, "script_dual_ma_out1_slope_pct", percentDistance(out1, smoothOut1), smoothOut1 != 0)
	setValue(values, "script_dual_ma_out2_slope_pct", percentDistance(out2, prevOut2), prevOut2 != 0)
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

func addScriptMovingAverageSignal(values map[string]string, signals map[string]string, closes []float64) {
	if len(closes) < 28 {
		return
	}
	ema10, ok10 := ema(closes, 10)
	breakthrough, okBreakthrough := ema(closes[:len(closes)-1], 13)
	ema12, ok12 := ema(closes, 12)
	ema26, ok26 := ema(closes, 26)
	prevEMA10, okPrev10 := ema(closes[:len(closes)-1], 10)
	prevBreakthrough, okPrevBreakthrough := ema(closes[:len(closes)-2], 13)
	if !ok10 || !okBreakthrough || !ok12 || !ok26 || !okPrev10 || !okPrevBreakthrough || breakthrough == 0 || prevBreakthrough == 0 {
		return
	}
	a1x := (ema10 - breakthrough) / breakthrough * 100
	prevA1x := (prevEMA10 - prevBreakthrough) / prevBreakthrough * 100
	midDirection := ema12 - ema26
	setValue(values, "script_ma_breakout_pct", a1x, true)
	setValue(values, "script_ma_mid_direction", midDirection, true)
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

func addEMDFeatures(values map[string]string, signals map[string]string, closes []float64, period int, multiplier float64) {
	avgSeries, ok := smmaSeries(closes, period)
	if !ok || len(avgSeries) < period+2 {
		return
	}
	offset := len(closes) - len(avgSeries)
	deviations := make([]float64, 0, len(avgSeries))
	for index, avg := range avgSeries {
		deviations = append(deviations, math.Abs(closes[index+offset]-avg))
	}
	emdSeries, ok := emaSeries(deviations, period)
	if !ok || len(emdSeries) < 2 {
		return
	}
	avg := avgSeries[len(avgSeries)-1]
	emd := emdSeries[len(emdSeries)-1]
	upper := avg + emd*multiplier
	lower := avg - emd*multiplier
	previousAvg := avgSeries[len(avgSeries)-2]
	previousEMD := emdSeries[len(emdSeries)-2]
	previousUpper := previousAvg + previousEMD*multiplier
	previousLower := previousAvg - previousEMD*multiplier
	current := closes[len(closes)-1]
	previous := closes[len(closes)-2]

	setValue(values, "emd_avg", avg, true)
	setValue(values, "emd_value", emd, true)
	setValue(values, "emd_upper", upper, true)
	setValue(values, "emd_lower", lower, true)
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

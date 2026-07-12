package indicatorcalc

import "math"

func addSqueezeMomentum(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	addSqueezeMomentumToSet(nil, values, signals, highs, lows, closes)
}

func addSqueezeMomentumToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	const (
		length   = 20
		multKC   = 1.5
		lengthKC = 20
	)
	basis, ok := sma(closes, length)
	if !ok {
		return
	}
	deviation, ok := standardDeviation(closes, length)
	if !ok {
		return
	}
	bbUpper := basis + multKC*deviation
	bbLower := basis - multKC*deviation

	ma, ok := sma(closes, lengthKC)
	if !ok {
		return
	}
	rangeMA, ok := recentTrueRangeMean(highs, lows, closes, lengthKC)
	if !ok {
		return
	}
	kcUpper := ma + rangeMA*multKC
	kcLower := ma - rangeMA*multKC
	squeeze := "off"
	switch {
	case bbLower > kcLower && bbUpper < kcUpper:
		squeeze = "on"
	case bbLower < kcLower && bbUpper > kcUpper:
		squeeze = "released"
	}
	signals["squeeze"] = squeeze
	momentum, previous, ok := squeezeMomentum(highs, lows, closes, lengthKC)
	if !ok {
		return
	}
	setValueTarget(target, values, "squeeze_momentum", momentum, true)
	setValueTarget(target, values, "squeeze_momentum_delta", momentum-previous, true)
	signals["squeeze_state"] = squeezeState(squeeze, momentum, previous)
	switch {
	case momentum > 0 && momentum >= previous:
		signals["momentum_state"] = "bull"
	case momentum > 0:
		signals["momentum_state"] = "bull_fading"
	case momentum < 0 && momentum <= previous:
		signals["momentum_state"] = "bear"
	case momentum < 0:
		signals["momentum_state"] = "bear_fading"
	default:
		signals["momentum_state"] = "flat"
	}
}

func recentTrueRangeMean(highs []float64, lows []float64, closes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) <= period || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, false
	}
	start := len(closes) - period
	total := 0.0
	for index := start; index < len(closes); index++ {
		total += maxFloat(
			highs[index]-lows[index],
			absFloat(highs[index]-closes[index-1]),
			absFloat(lows[index]-closes[index-1]),
		)
	}
	return total / float64(period), true
}

func squeezeState(squeeze string, momentum float64, previous float64) string {
	direction := "flat"
	switch {
	case momentum > 0 && momentum >= previous:
		direction = "up"
	case momentum < 0 && momentum <= previous:
		direction = "down"
	}
	switch squeeze {
	case "on":
		return "squeeze_on"
	case "released":
		return "release_" + direction
	default:
		return "off_" + direction
	}
}

func addBollingerFeatures(values map[string]string, signals map[string]string, closes []float64) {
	addBollingerFeaturesWithContext(values, signals, closes, nil)
}

func addBollingerFeaturesWithContext(values map[string]string, signals map[string]string, closes []float64, features *featureContext) {
	addBollingerFeaturesWithContextToSet(nil, values, signals, closes, features)
}

func addBollingerFeaturesWithContextToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, features *featureContext) {
	upper, middle, lower, ok := 0.0, 0.0, 0.0, false
	if features != nil {
		upper, middle, lower, ok = features.bollinger(20, 2)
	} else {
		upper, middle, lower, ok = bollinger(closes, 20, 2)
	}
	if !ok || middle == 0 || upper == lower {
		return
	}
	last := closes[len(closes)-1]
	width := (upper - lower) / middle * 100
	setValueTarget(target, values, "bb_width_pct", width, true)
	setValueTarget(target, values, "bb_percent_b", (last-lower)/(upper-lower), true)
	addBollingerShapeFeaturesToSet(target, values, signals, closes, width)
	switch {
	case last > upper:
		signals["bb_position"] = "above_upper"
	case last < lower:
		signals["bb_position"] = "below_lower"
	default:
		signals["bb_position"] = "inside"
	}
}

func addBollingerShapeFeatures(values map[string]string, signals map[string]string, closes []float64, currentWidth float64) {
	addBollingerShapeFeaturesToSet(nil, values, signals, closes, currentWidth)
}

func addBollingerShapeFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, currentWidth float64) {
	if len(closes) < 25 {
		return
	}
	prevUpper, prevMiddle, prevLower, ok := bollinger(closes[:len(closes)-5], 20, 2)
	if !ok || prevMiddle == 0 {
		return
	}
	upper, middle, lower, ok := bollinger(closes, 20, 2)
	if !ok {
		return
	}
	previousWidth := (prevUpper - prevLower) / prevMiddle * 100
	widthDelta := currentWidth - previousWidth
	setValueTarget(target, values, "bb_width_delta", widthDelta, true)
	setValueTarget(target, values, "bb_middle_slope_pct", percentDistance(middle, prevMiddle), prevMiddle != 0)
	setValueTarget(target, values, "bb_upper_slope_pct", percentDistance(upper, prevUpper), prevUpper != 0)
	setValueTarget(target, values, "bb_lower_slope_pct", percentDistance(lower, prevLower), prevLower != 0)
	signals["bb_width_state"] = bollingerWidthState(widthDelta, previousWidth)
	signals["bb_trend"] = bollingerTrend(percentDistance(middle, prevMiddle))
}

func addChannelFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	addChannelFeaturesWithContext(values, signals, highs, lows, closes, nil)
}

func addChannelFeaturesWithContext(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, features *featureContext) {
	addChannelFeaturesWithContextToSet(nil, values, signals, highs, lows, closes, features)
}

func addChannelFeaturesWithContextToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, features *featureContext) {
	if features != nil {
		if upper, lower, ok := features.donchian(20); ok {
			addDonchianChannelFeaturesWithRangeToSet(target, values, signals, highs, lows, closes, 20, upper, lower)
		} else {
			addDonchianChannelFeatures(values, signals, highs, lows, closes, 20)
		}
	} else {
		addDonchianChannelFeatures(values, signals, highs, lows, closes, 20)
	}
	if features != nil {
		atrValue, atrOK := features.atrValue(20)
		middle, middleOK := features.emaValue(20)
		if atrOK && middleOK {
			addKeltnerChannelFeaturesWithValuesToSet(target, values, signals, closes, middle, atrValue, 2)
			return
		}
	}
	addKeltnerChannelFeatures(values, signals, highs, lows, closes, 20, 20, 2)
}

func addDonchianChannelFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int) {
	upper, lower, ok := donchian(highs, lows, period)
	if !ok || len(closes) == 0 {
		return
	}
	addDonchianChannelFeaturesWithRange(values, signals, highs, lows, closes, period, upper, lower)
}

func addDonchianChannelFeaturesWithRange(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, upper float64, lower float64) {
	addDonchianChannelFeaturesWithRangeToSet(nil, values, signals, highs, lows, closes, period, upper, lower)
}

func addDonchianChannelFeaturesWithRangeToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, upper float64, lower float64) {
	if len(closes) == 0 {
		return
	}
	middle := (upper + lower) / 2
	last := closes[len(closes)-1]
	setValueTarget(target, values, "donchian_high20", upper, true)
	setValueTarget(target, values, "donchian_low20", lower, true)
	setValueTarget(target, values, "donchian_mid20", middle, true)
	setValueTarget(target, values, "donchian_width_pct20", (upper-lower)/middle*100, middle != 0)
	setValueTarget(target, values, "donchian_position20", (last-lower)/(upper-lower), upper != lower)
	breakoutUpper := upper
	breakoutLower := lower
	if len(highs) > period && len(lows) > period {
		if previousUpper, previousLower, previousOK := donchian(highs[:len(highs)-1], lows[:len(lows)-1], period); previousOK {
			breakoutUpper = previousUpper
			breakoutLower = previousLower
		}
	}
	signals["donchian_breakout"] = channelBreakout(last, breakoutUpper, breakoutLower)
}

func addKeltnerChannelFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, emaPeriod int, atrPeriod int, multiplier float64) {
	atrValue, ok := atr(highs, lows, closes, atrPeriod)
	if !ok || len(closes) == 0 {
		return
	}
	addKeltnerChannelFeaturesWithATR(values, signals, closes, emaPeriod, atrValue, multiplier)
}

func addKeltnerChannelFeaturesWithATR(values map[string]string, signals map[string]string, closes []float64, emaPeriod int, atrValue float64, multiplier float64) {
	middle, ok := ema(closes, emaPeriod)
	if !ok || len(closes) == 0 {
		return
	}
	addKeltnerChannelFeaturesWithValues(values, signals, closes, middle, atrValue, multiplier)
}

func addKeltnerChannelFeaturesWithValues(values map[string]string, signals map[string]string, closes []float64, middle float64, atrValue float64, multiplier float64) {
	addKeltnerChannelFeaturesWithValuesToSet(nil, values, signals, closes, middle, atrValue, multiplier)
}

func addKeltnerChannelFeaturesWithValuesToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, middle float64, atrValue float64, multiplier float64) {
	if len(closes) == 0 {
		return
	}
	upper := middle + atrValue*multiplier
	lower := middle - atrValue*multiplier
	last := closes[len(closes)-1]
	setValueTarget(target, values, "keltner_upper20", upper, true)
	setValueTarget(target, values, "keltner_middle20", middle, true)
	setValueTarget(target, values, "keltner_lower20", lower, true)
	setValueTarget(target, values, "keltner_width_pct20", (upper-lower)/middle*100, middle != 0)
	setValueTarget(target, values, "keltner_position20", (last-lower)/(upper-lower), upper != lower)
	signals["keltner_breakout"] = channelBreakout(last, upper, lower)
}

func channelBreakout(value float64, upper float64, lower float64) string {
	switch {
	case value > upper:
		return "breakout_up"
	case value < lower:
		return "breakout_down"
	default:
		return "inside"
	}
}

func bollingerWidthState(delta float64, previousWidth float64) string {
	threshold := 0.05
	if previousWidth > 0 {
		threshold = previousWidth * 0.08
	}
	switch {
	case delta > threshold:
		return "expanding"
	case delta < -threshold:
		return "contracting"
	default:
		return "flat"
	}
}

func bollingerTrend(middleSlopePct float64) string {
	switch {
	case middleSlopePct > 0.08:
		return "up"
	case middleSlopePct < -0.08:
		return "down"
	default:
		return "flat"
	}
}

func squeezeMomentum(highs []float64, lows []float64, closes []float64, period int) (float64, float64, bool) {
	if period <= 0 || len(closes) < period*2 || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, 0, false
	}
	current, ok := squeezeMomentumAt(highs, lows, closes, period, len(closes))
	if !ok {
		return 0, 0, false
	}
	previous, ok := squeezeMomentumAt(highs, lows, closes, period, len(closes)-1)
	if !ok {
		return 0, 0, false
	}
	return current, previous, true
}

func squeezeMomentumAt(highs []float64, lows []float64, closes []float64, period int, end int) (float64, bool) {
	value, ok := squeezeMomentumAtCompact(highs, lows, closes, period, end)
	if ok {
		return value, true
	}
	return squeezeMomentumAtBatch(highs, lows, closes, period, end)
}

func squeezeMomentumAtCompact(highs []float64, lows []float64, closes []float64, period int, end int) (float64, bool) {
	if end < period*2 || end > len(closes) || len(highs) < end || len(lows) < end {
		return 0, false
	}
	highWindow := newFloatMonotonicWindow(true)
	lowWindow := newFloatMonotonicWindow(false)
	if !highWindow.canHold(period) || !lowWindow.canHold(period) {
		return 0, false
	}
	start := end - period
	warmupStart := start - period + 1
	source := make([]float64, 0, period)
	closeSum := 0.0
	for index := warmupStart; index < end; index++ {
		highWindow.push(index, highs[index])
		lowWindow.push(index, lows[index])
		closeSum += closes[index]
		if index >= warmupStart+period {
			closeSum -= closes[index-period]
		}
		if index < start {
			continue
		}
		highWindow.expireBefore(index - period + 1)
		lowWindow.expireBefore(index - period + 1)
		highest, okHigh := highWindow.value()
		lowest, okLow := lowWindow.value()
		if !okHigh || !okLow {
			return 0, false
		}
		closeMA := closeSum / float64(period)
		baseline := ((highest+lowest)/2 + closeMA) / 2
		source = append(source, closes[index]-baseline)
	}
	return linearRegression(source, period, 0)
}

func squeezeMomentumAtBatch(highs []float64, lows []float64, closes []float64, period int, end int) (float64, bool) {
	if end < period*2 || end > len(closes) || len(highs) < end || len(lows) < end {
		return 0, false
	}
	source := make([]float64, 0, period)
	start := end - period
	for index := start; index < end; index++ {
		highest, lowest := highLow(highs[index-period+1:index+1], lows[index-period+1:index+1])
		closeMA, ok := sma(closes[:index+1], period)
		if !ok {
			return 0, false
		}
		baseline := ((highest+lowest)/2 + closeMA) / 2
		source = append(source, closes[index]-baseline)
	}
	return linearRegression(source, period, 0)
}

func standardDeviation(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period {
		return 0, false
	}
	average, _ := sma(values, period)
	window := values[len(values)-period:]
	var variance float64
	for _, value := range window {
		diff := value - average
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(period)), true
}

func trueRangeSeries(highs []float64, lows []float64, closes []float64) []float64 {
	values := make([]float64, 0, len(closes))
	for index := range closes {
		if index == 0 {
			values = append(values, highs[index]-lows[index])
			continue
		}
		values = append(values, maxFloat(
			highs[index]-lows[index],
			absFloat(highs[index]-closes[index-1]),
			absFloat(lows[index]-closes[index-1]),
		))
	}
	return values
}

func linearRegression(values []float64, period int, offset int) (float64, bool) {
	if period <= 0 || len(values) < period || offset < 0 || offset >= period {
		return 0, false
	}
	window := values[len(values)-period:]
	var sumX float64
	var sumY float64
	var sumXY float64
	var sumXX float64
	for index, value := range window {
		x := float64(index)
		sumX += x
		sumY += value
		sumXY += x * value
		sumXX += x * x
	}
	count := float64(period)
	denominator := count*sumXX - sumX*sumX
	if denominator == 0 {
		return 0, false
	}
	slope := (count*sumXY - sumX*sumY) / denominator
	intercept := (sumY - slope*sumX) / count
	return intercept + slope*float64(period-1-offset), true
}

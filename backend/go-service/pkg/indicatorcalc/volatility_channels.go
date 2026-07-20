package indicatorcalc

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

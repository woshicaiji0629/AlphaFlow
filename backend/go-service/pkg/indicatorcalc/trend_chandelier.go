package indicatorcalc

func addChandelierExit(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64) {
	addChandelierExitToSet(nil, values, signals, highs, lows, closes, period, multiplier)
}

func addChandelierExitToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64) {
	longStop, shortStop, ok := chandelierExit(highs, lows, closes, period, multiplier)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValueTarget(target, values, "chandelier_long", longStop, true)
	setValueTarget(target, values, "chandelier_short", shortStop, true)
	switch {
	case last >= longStop:
		signals["chandelier_direction"] = "up"
		setValueTarget(target, values, "chandelier_stop_distance_pct", absFloat(percentDistance(last, longStop)), longStop != 0)
	case last <= shortStop:
		signals["chandelier_direction"] = "down"
		setValueTarget(target, values, "chandelier_stop_distance_pct", absFloat(percentDistance(last, shortStop)), shortStop != 0)
	default:
		signals["chandelier_direction"] = "neutral"
	}
}

func chandelierExit(highs []float64, lows []float64, closes []float64, period int, multiplier float64) (float64, float64, bool) {
	if period <= 0 || len(closes) < period || multiplier <= 0 {
		return 0, 0, false
	}
	atrValue, ok := atr(highs, lows, closes, period)
	if !ok {
		return 0, 0, false
	}
	highest, lowest := highLow(highs[len(highs)-period:], lows[len(lows)-period:])
	return highest - multiplier*atrValue, lowest + multiplier*atrValue, true
}

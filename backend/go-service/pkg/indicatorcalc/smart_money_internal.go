package indicatorcalc

func addInternalSmartMoney(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	period := minInt(25, len(closes))
	if period < 7 {
		return
	}
	start := len(closes) - period
	last := len(closes) - 1
	pivotHighs, pivotLows := pivots(highs[start:], lows[start:], 1)
	internalHigh, okHigh := recentSwing(pivotHighs)
	internalLow, okLow := recentSwing(pivotLows)
	if !okHigh || !okLow {
		return
	}
	setValueTarget(target, values, "internal_swing_high", internalHigh.price, true)
	setValueTarget(target, values, "internal_swing_low", internalLow.price, true)
	setValueTarget(target, values, "internal_swing_high_distance_pct", percentDistance(closes[last], internalHigh.price), internalHigh.price != 0)
	setValueTarget(target, values, "internal_swing_low_distance_pct", percentDistance(closes[last], internalLow.price), internalLow.price != 0)

	trend := detectSwingTrend(pivotHighs, pivotLows)
	bias := structureBias(trend)
	highStrength, lowStrength := swingStrengthLabels(trend)
	signals["internal_swing_high_strength"] = highStrength
	signals["internal_swing_low_strength"] = lowStrength
	event := "none"
	switch {
	case closes[last] > internalHigh.price:
		bias = "bull"
		if trend == swingTrendDown {
			event = "choch_up"
		} else {
			event = "bos_up"
		}
	case closes[last] < internalLow.price:
		bias = "bear"
		if trend == swingTrendUp {
			event = "choch_down"
		} else {
			event = "bos_down"
		}
	case highs[last] > internalHigh.price && closes[last] < internalHigh.price:
		event = "sweep_high"
	case lows[last] < internalLow.price && closes[last] > internalLow.price:
		event = "sweep_low"
	}
	signals["internal_structure_event"] = event
	signals["internal_structure_bias"] = bias
}

func swingStrengthLabels(trend swingTrend) (string, string) {
	switch trend {
	case swingTrendUp:
		return "weak", "strong"
	case swingTrendDown:
		return "strong", "weak"
	default:
		return "unknown", "unknown"
	}
}

func addEqualHighLow(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int) {
	pivotHighs, pivotLows := pivots(highs[len(highs)-period:], lows[len(lows)-period:], 2)
	addEqualHighLowWithPivots(target, values, signals, highs, lows, closes, period, pivotHighs, pivotLows)
}

func addEqualHighLowWithPivots(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, pivotHighs []priceLevel, pivotLows []priceLevel) {
	tolerance := equalHighLowTolerance(highs, lows, closes, period)
	if level, ok := recentEqualLevel(pivotHighs, tolerance); ok {
		setValueTarget(target, values, "equal_high", level, true)
		setValueTarget(target, values, "equal_high_distance_pct", percentDistance(closes[len(closes)-1], level), level != 0)
		signals["equal_high_low"] = "equal_high"
	}
	if level, ok := recentEqualLevel(pivotLows, tolerance); ok {
		setValueTarget(target, values, "equal_low", level, true)
		setValueTarget(target, values, "equal_low_distance_pct", percentDistance(closes[len(closes)-1], level), level != 0)
		if signals["equal_high_low"] == "equal_high" {
			signals["equal_high_low"] = "both"
		} else {
			signals["equal_high_low"] = "equal_low"
		}
	}
	if signals["equal_high_low"] == "" {
		signals["equal_high_low"] = "none"
	}
}

func equalHighLowTolerance(highs []float64, lows []float64, closes []float64, period int) float64 {
	atrValue, ok := atr(highs, lows, closes, minInt(14, period-1))
	if ok && atrValue > 0 {
		return atrValue * 0.1
	}
	return closes[len(closes)-1] * 0.001
}

func recentEqualLevel(levels []priceLevel, tolerance float64) (float64, bool) {
	if len(levels) < 2 {
		return 0, false
	}
	latest := levels[len(levels)-1]
	for index := len(levels) - 2; index >= 0; index-- {
		if absFloat(latest.price-levels[index].price) <= tolerance {
			return (latest.price + levels[index].price) / 2, true
		}
	}
	return 0, false
}

func addFairValueGap(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	if len(closes) < 3 {
		return
	}
	last := len(closes) - 1
	bullish := lows[last] > highs[last-2] && closes[last-1] > highs[last-2]
	bearish := highs[last] < lows[last-2] && closes[last-1] < lows[last-2]
	switch {
	case bullish:
		top := lows[last]
		bottom := highs[last-2]
		setFairValueGap(target, values, signals, top, bottom, closes[last], "bull")
	case bearish:
		top := lows[last-2]
		bottom := highs[last]
		setFairValueGap(target, values, signals, top, bottom, closes[last], "bear")
	default:
		signals["fvg_direction"] = "none"
		signals["fvg_position"] = "none"
	}
}

func setFairValueGap(target *ValueSet, values map[string]string, signals map[string]string, top float64, bottom float64, last float64, direction string) {
	mid := (top + bottom) / 2
	setValueTarget(target, values, "fvg_top", top, true)
	setValueTarget(target, values, "fvg_bottom", bottom, true)
	setValueTarget(target, values, "fvg_mid", mid, true)
	setValueTarget(target, values, "fvg_distance_pct", percentDistance(last, mid), mid != 0)
	signals["fvg_direction"] = direction
	switch {
	case last > top:
		signals["fvg_position"] = "above"
	case last < bottom:
		signals["fvg_position"] = "below"
	default:
		signals["fvg_position"] = "inside"
	}
}

func addPremiumDiscountZones(target *ValueSet, values map[string]string, signals map[string]string, last float64, swingHigh float64, swingLow float64) {
	if swingHigh <= swingLow || swingHigh == 0 || swingLow == 0 {
		return
	}
	premium := 0.95*swingHigh + 0.05*swingLow
	discount := 0.95*swingLow + 0.05*swingHigh
	equilibrium := (swingHigh + swingLow) / 2
	setValueTarget(target, values, "premium_level", premium, true)
	setValueTarget(target, values, "discount_level", discount, true)
	setValueTarget(target, values, "equilibrium_level", equilibrium, true)
	switch {
	case last >= premium:
		signals["premium_discount_zone"] = "premium"
	case last <= discount:
		signals["premium_discount_zone"] = "discount"
	default:
		signals["premium_discount_zone"] = "equilibrium"
	}
}

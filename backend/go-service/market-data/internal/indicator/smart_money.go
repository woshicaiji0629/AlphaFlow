package indicator

type swingTrend int

const (
	swingTrendRange swingTrend = iota
	swingTrendUp
	swingTrendDown
)

func addSmartMoney(values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64) {
	period := minInt(60, len(closes))
	if period < 7 {
		return
	}
	start := len(closes) - period
	last := len(closes) - 1
	windowHighs := highs[start:]
	windowLows := lows[start:]
	pivotHighs, pivotLows := pivots(windowHighs, windowLows, 2)
	swingHigh, okHigh := recentSwing(pivotHighs)
	swingLow, okLow := recentSwing(pivotLows)
	if !okHigh || !okLow {
		high, low := highLow(highs[start:last], lows[start:last])
		swingHigh = priceLevel{price: high, recency: last - start - 1}
		swingLow = priceLevel{price: low, recency: last - start - 1}
	}

	setValue(values, "swing_high", swingHigh.price, swingHigh.price > 0)
	setValue(values, "swing_low", swingLow.price, swingLow.price > 0)
	setValue(values, "swing_high_distance_pct", percentDistance(closes[last], swingHigh.price), swingHigh.price != 0)
	setValue(values, "swing_low_distance_pct", percentDistance(closes[last], swingLow.price), swingLow.price != 0)

	trend := detectSwingTrend(pivotHighs, pivotLows)
	direction := ""
	structureEvent := "none"
	structureBias := structureBias(trend)
	switch {
	case swingHigh.price > 0 && closes[last] > swingHigh.price:
		direction = "up"
		structureBias = "bull"
		if trend == swingTrendDown {
			signals["choch"] = "up"
			structureEvent = "choch_up"
		} else {
			signals["market_structure"] = "bos_up"
			structureEvent = "bos_up"
		}
	case swingLow.price > 0 && closes[last] < swingLow.price:
		direction = "down"
		structureBias = "bear"
		if trend == swingTrendUp {
			signals["choch"] = "down"
			structureEvent = "choch_down"
		} else {
			signals["market_structure"] = "bos_down"
			structureEvent = "bos_down"
		}
	case swingHigh.price > 0 && highs[last] > swingHigh.price && closes[last] < swingHigh.price:
		signals["market_structure"] = "range"
		signals["smart_money"] = "liquidity_sweep_high"
		structureEvent = "sweep_high"
	case swingLow.price > 0 && lows[last] < swingLow.price && closes[last] > swingLow.price:
		signals["market_structure"] = "range"
		signals["smart_money"] = "liquidity_sweep_low"
		structureEvent = "sweep_low"
	default:
		signals["market_structure"] = "range"
	}
	signals["structure_event"] = structureEvent
	signals["structure_bias"] = structureBias

	blockHigh, blockLow, ok := orderBlock(opens, highs, lows, closes, start, last, direction)
	if ok {
		setValue(values, "order_block_high", blockHigh, true)
		setValue(values, "order_block_low", blockLow, true)
		setValue(values, "order_block_mid", (blockHigh+blockLow)/2, true)
	}
	addInternalSmartMoney(values, signals, highs, lows, closes)
	addEqualHighLow(values, signals, highs, lows, closes, period)
	addFairValueGap(values, signals, highs, lows, closes)
	addPremiumDiscountZones(values, signals, closes[last], swingHigh.price, swingLow.price)
}

func recentSwing(levels []priceLevel) (priceLevel, bool) {
	if len(levels) == 0 {
		return priceLevel{}, false
	}
	recent := levels[0]
	for _, level := range levels[1:] {
		if level.recency > recent.recency {
			recent = level
		}
	}
	return recent, true
}

func detectSwingTrend(highs []priceLevel, lows []priceLevel) swingTrend {
	if len(highs) < 2 || len(lows) < 2 {
		return swingTrendRange
	}
	prevHigh, lastHigh := lastTwoSwings(highs)
	prevLow, lastLow := lastTwoSwings(lows)
	switch {
	case lastHigh.price > prevHigh.price && lastLow.price > prevLow.price:
		return swingTrendUp
	case lastHigh.price < prevHigh.price && lastLow.price < prevLow.price:
		return swingTrendDown
	default:
		return swingTrendRange
	}
}

func structureBias(trend swingTrend) string {
	switch trend {
	case swingTrendUp:
		return "bull"
	case swingTrendDown:
		return "bear"
	default:
		return "range"
	}
}

func lastTwoSwings(levels []priceLevel) (priceLevel, priceLevel) {
	first := levels[0]
	second := levels[1]
	if first.recency > second.recency {
		first, second = second, first
	}
	for _, level := range levels[2:] {
		if level.recency > second.recency {
			first = second
			second = level
		} else if level.recency > first.recency {
			first = level
		}
	}
	return first, second
}

func orderBlock(opens []float64, highs []float64, lows []float64, closes []float64, start int, last int, direction string) (float64, float64, bool) {
	if last <= 0 {
		return 0, 0, false
	}
	for index := last - 1; index >= start; index-- {
		switch {
		case direction == "up" && closes[index] < opens[index]:
			return highs[index], lows[index], true
		case direction == "down" && closes[index] > opens[index]:
			return highs[index], lows[index], true
		}
	}
	return highs[last-1], lows[last-1], true
}

func addInternalSmartMoney(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
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
	setValue(values, "internal_swing_high", internalHigh.price, true)
	setValue(values, "internal_swing_low", internalLow.price, true)
	setValue(values, "internal_swing_high_distance_pct", percentDistance(closes[last], internalHigh.price), internalHigh.price != 0)
	setValue(values, "internal_swing_low_distance_pct", percentDistance(closes[last], internalLow.price), internalLow.price != 0)

	trend := detectSwingTrend(pivotHighs, pivotLows)
	bias := structureBias(trend)
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

func addEqualHighLow(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int) {
	pivotHighs, pivotLows := pivots(highs[len(highs)-period:], lows[len(lows)-period:], 2)
	tolerance := equalHighLowTolerance(highs, lows, closes, period)
	if level, ok := recentEqualLevel(pivotHighs, tolerance); ok {
		setValue(values, "equal_high", level, true)
		setValue(values, "equal_high_distance_pct", percentDistance(closes[len(closes)-1], level), level != 0)
		signals["equal_high_low"] = "equal_high"
	}
	if level, ok := recentEqualLevel(pivotLows, tolerance); ok {
		setValue(values, "equal_low", level, true)
		setValue(values, "equal_low_distance_pct", percentDistance(closes[len(closes)-1], level), level != 0)
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

func addFairValueGap(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
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
		setFairValueGap(values, signals, top, bottom, closes[last], "bull")
	case bearish:
		top := lows[last-2]
		bottom := highs[last]
		setFairValueGap(values, signals, top, bottom, closes[last], "bear")
	default:
		signals["fvg_direction"] = "none"
		signals["fvg_position"] = "none"
	}
}

func setFairValueGap(values map[string]string, signals map[string]string, top float64, bottom float64, last float64, direction string) {
	mid := (top + bottom) / 2
	setValue(values, "fvg_top", top, true)
	setValue(values, "fvg_bottom", bottom, true)
	setValue(values, "fvg_mid", mid, true)
	setValue(values, "fvg_distance_pct", percentDistance(last, mid), mid != 0)
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

func addPremiumDiscountZones(values map[string]string, signals map[string]string, last float64, swingHigh float64, swingLow float64) {
	if swingHigh <= swingLow || swingHigh == 0 || swingLow == 0 {
		return
	}
	premium := 0.95*swingHigh + 0.05*swingLow
	discount := 0.95*swingLow + 0.05*swingHigh
	equilibrium := (swingHigh + swingLow) / 2
	setValue(values, "premium_level", premium, true)
	setValue(values, "discount_level", discount, true)
	setValue(values, "equilibrium_level", equilibrium, true)
	switch {
	case last >= premium:
		signals["premium_discount_zone"] = "premium"
	case last <= discount:
		signals["premium_discount_zone"] = "discount"
	default:
		signals["premium_discount_zone"] = "equilibrium"
	}
}

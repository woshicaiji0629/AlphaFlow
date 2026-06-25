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
	switch {
	case swingHigh.price > 0 && closes[last] > swingHigh.price:
		direction = "up"
		if trend == swingTrendDown {
			signals["choch"] = "up"
		} else {
			signals["market_structure"] = "bos_up"
		}
	case swingLow.price > 0 && closes[last] < swingLow.price:
		direction = "down"
		if trend == swingTrendUp {
			signals["choch"] = "down"
		} else {
			signals["market_structure"] = "bos_down"
		}
	case swingHigh.price > 0 && highs[last] > swingHigh.price && closes[last] < swingHigh.price:
		signals["market_structure"] = "range"
		signals["smart_money"] = "liquidity_sweep_high"
	case swingLow.price > 0 && lows[last] < swingLow.price && closes[last] > swingLow.price:
		signals["market_structure"] = "range"
		signals["smart_money"] = "liquidity_sweep_low"
	default:
		signals["market_structure"] = "range"
	}

	blockHigh, blockLow, ok := orderBlock(opens, highs, lows, closes, start, last, direction)
	if ok {
		setValue(values, "order_block_high", blockHigh, true)
		setValue(values, "order_block_low", blockLow, true)
		setValue(values, "order_block_mid", (blockHigh+blockLow)/2, true)
	}
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

package indicatorcalc

type swingTrend int

const (
	swingTrendRange swingTrend = iota
	swingTrendUp
	swingTrendDown
)

type liquiditySweepState struct {
	kind   string
	level  float64
	top    float64
	bottom float64
	age    int
}

type momentumSupplyDemandZone struct {
	top        float64
	bottom     float64
	startIndex int
	breakIndex int
	ok         bool
}

type momentumSupplyDemandState struct {
	supply      momentumSupplyDemandZone
	demand      momentumSupplyDemandZone
	position    string
	retestEvent string
	breakEvent  string
}

func addSmartMoney(values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64) {
	addSmartMoneyToSet(nil, values, signals, opens, highs, lows, closes)
}

func addSmartMoneyToSet(target *ValueSet, values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64) {
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

	setValueTarget(target, values, "swing_high", swingHigh.price, swingHigh.price > 0)
	setValueTarget(target, values, "swing_low", swingLow.price, swingLow.price > 0)
	setValueTarget(target, values, "swing_high_distance_pct", percentDistance(closes[last], swingHigh.price), swingHigh.price != 0)
	setValueTarget(target, values, "swing_low_distance_pct", percentDistance(closes[last], swingLow.price), swingLow.price != 0)

	trend := detectSwingTrend(pivotHighs, pivotLows)
	direction := ""
	structureEvent := "none"
	structureBias := structureBias(trend)
	highStrength, lowStrength := swingStrengthLabels(trend)
	signals["swing_high_strength"] = highStrength
	signals["swing_low_strength"] = lowStrength
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
	if sweep, ok := detectLiquiditySweep(windowHighs, windowLows, closes[start:], pivotHighs, pivotLows); ok {
		setValueTarget(target, values, "liquidity_sweep_level", sweep.level, true)
		setValueTarget(target, values, "liquidity_sweep_top", sweep.top, true)
		setValueTarget(target, values, "liquidity_sweep_bottom", sweep.bottom, true)
		setValueTarget(target, values, "liquidity_sweep_age", float64(sweep.age), true)
		signals["liquidity_sweep_type"] = sweep.kind
		if structureEvent == "none" {
			if liquiditySweepIsHigh(sweep.kind) {
				signals["smart_money"] = "liquidity_sweep_high"
				structureEvent = "sweep_high"
			} else if liquiditySweepIsLow(sweep.kind) {
				signals["smart_money"] = "liquidity_sweep_low"
				structureEvent = "sweep_low"
			}
		}
	} else {
		signals["liquidity_sweep_type"] = "none"
	}
	if momentumSD, ok := detectMomentumSupplyDemandZones(opens, highs, lows, closes, 120, 4, 4, 0.5, 20, 1.5); ok {
		addMomentumSupplyDemandValues(target, values, signals, momentumSD, last)
	}
	signals["structure_event"] = structureEvent
	signals["structure_bias"] = structureBias

	blockHigh, blockLow, ok := orderBlock(opens, highs, lows, closes, start, last, direction)
	if ok {
		setValueTarget(target, values, "order_block_high", blockHigh, true)
		setValueTarget(target, values, "order_block_low", blockLow, true)
		setValueTarget(target, values, "order_block_mid", (blockHigh+blockLow)/2, true)
	}
	addInternalSmartMoney(target, values, signals, highs, lows, closes)
	addEqualHighLow(target, values, signals, highs, lows, closes, period)
	addFairValueGap(target, values, signals, highs, lows, closes)
	addPremiumDiscountZones(target, values, signals, closes[last], swingHigh.price, swingLow.price)
}

func addMomentumSupplyDemandValues(target *ValueSet, values map[string]string, signals map[string]string, state momentumSupplyDemandState, last int) {
	if state.supply.ok {
		setValueTarget(target, values, "momentum_supply_top", state.supply.top, true)
		setValueTarget(target, values, "momentum_supply_bottom", state.supply.bottom, true)
		setValueTarget(target, values, "momentum_supply_mid", (state.supply.top+state.supply.bottom)/2, true)
		setValueTarget(target, values, "momentum_supply_age", float64(last-state.supply.startIndex), true)
	}
	if state.demand.ok {
		setValueTarget(target, values, "momentum_demand_top", state.demand.top, true)
		setValueTarget(target, values, "momentum_demand_bottom", state.demand.bottom, true)
		setValueTarget(target, values, "momentum_demand_mid", (state.demand.top+state.demand.bottom)/2, true)
		setValueTarget(target, values, "momentum_demand_age", float64(last-state.demand.startIndex), true)
	}
	signals["momentum_sd_position"] = state.position
	signals["momentum_sd_retest"] = state.retestEvent
	signals["momentum_sd_break"] = state.breakEvent
}

func detectMomentumSupplyDemandZones(
	opens []float64,
	highs []float64,
	lows []float64,
	closes []float64,
	lookback int,
	momentumSpan int,
	momentumCount int,
	bodyMultiplier float64,
	atrPeriod int,
	maxZoneATR float64,
) (momentumSupplyDemandState, bool) {
	if lookback <= 0 || momentumSpan <= 0 || momentumCount <= 0 || bodyMultiplier <= 0 ||
		len(closes) < lookback || len(opens) != len(closes) || len(highs) != len(closes) || len(lows) != len(closes) {
		return momentumSupplyDemandState{}, false
	}
	atrValue, ok := atr(highs, lows, closes, atrPeriod)
	if !ok || atrValue <= 0 {
		return momentumSupplyDemandState{}, false
	}
	start := len(closes) - lookback
	last := len(closes) - 1
	minDistanceBetweenZones := 5
	lastDemandZone := start - minDistanceBetweenZones
	lastSupplyZone := start - minDistanceBetweenZones
	state := momentumSupplyDemandState{
		position:    "unknown",
		retestEvent: "none",
		breakEvent:  "none",
	}
	for index := start + momentumSpan + 1; index <= last; index++ {
		bullish, bearish := momentumCandleCounts(opens, closes, index, momentumSpan, bodyMultiplier)
		if bullish >= momentumCount && index-lastDemandZone > minDistanceBetweenZones {
			anchor := index - momentumSpan - 1
			state.demand = momentumSupplyDemandZoneFromRange(highs[anchor], lows[anchor], anchor, atrValue, maxZoneATR)
			lastDemandZone = index
		}
		if bearish >= momentumCount && index-lastSupplyZone > minDistanceBetweenZones {
			anchor := index - momentumSpan - 1
			state.supply = momentumSupplyDemandZoneFromRange(highs[anchor], lows[anchor], anchor, atrValue, maxZoneATR)
			lastSupplyZone = index
		}
		if state.demand.ok && state.demand.breakIndex < 0 && index > state.demand.startIndex && closes[index] < state.demand.bottom {
			state.demand.breakIndex = index
		}
		if state.supply.ok && state.supply.breakIndex < 0 && index > state.supply.startIndex && closes[index] > state.supply.top {
			state.supply.breakIndex = index
		}
	}
	state.position = momentumSupplyDemandPosition(closes[last], state)
	state.retestEvent = momentumSupplyDemandRetest(highs[last], lows[last], last, state)
	state.breakEvent = momentumSupplyDemandBreak(last, state)
	return state, state.supply.ok || state.demand.ok
}

func momentumCandleCounts(opens []float64, closes []float64, index int, momentumSpan int, bodyMultiplier float64) (int, int) {
	averageBody := averageBodySizeAt(opens, closes, index, 20)
	if averageBody <= 0 {
		return 0, 0
	}
	bullish := 0
	bearish := 0
	start := index - momentumSpan + 1
	for bodyIndex := start; bodyIndex <= index; bodyIndex++ {
		bodySize := absFloat(closes[bodyIndex] - opens[bodyIndex])
		if bodySize < averageBody*bodyMultiplier {
			continue
		}
		if closes[bodyIndex] > opens[bodyIndex] {
			bullish++
		} else if closes[bodyIndex] < opens[bodyIndex] {
			bearish++
		}
	}
	return bullish, bearish
}

func averageBodySizeAt(opens []float64, closes []float64, index int, period int) float64 {
	if period <= 0 || index < 0 || index >= len(closes) || len(opens) != len(closes) {
		return 0
	}
	start := index - period + 1
	if start < 0 {
		start = 0
	}
	var total float64
	for bodyIndex := start; bodyIndex <= index; bodyIndex++ {
		total += absFloat(closes[bodyIndex] - opens[bodyIndex])
	}
	return total / float64(index-start+1)
}

func momentumSupplyDemandZoneFromRange(high float64, low float64, startIndex int, atrValue float64, maxZoneATR float64) momentumSupplyDemandZone {
	top := high
	bottom := low
	maxSize := atrValue * maxZoneATR
	if maxSize > 0 && top-bottom > maxSize {
		diff := top - bottom - maxSize
		top -= diff / 2
		bottom += diff / 2
	}
	return momentumSupplyDemandZone{
		top:        top,
		bottom:     bottom,
		startIndex: startIndex,
		breakIndex: -1,
		ok:         top > bottom,
	}
}

func momentumSupplyDemandPosition(price float64, state momentumSupplyDemandState) string {
	if state.supply.ok {
		if price > state.supply.top {
			return "above_supply"
		}
		if price >= state.supply.bottom {
			return "in_supply"
		}
	}
	if state.demand.ok {
		if price < state.demand.bottom {
			return "below_demand"
		}
		if price <= state.demand.top {
			return "in_demand"
		}
	}
	if state.supply.ok || state.demand.ok {
		return "between_zones"
	}
	return "unknown"
}

func momentumSupplyDemandRetest(high float64, low float64, last int, state momentumSupplyDemandState) string {
	if state.supply.ok && state.supply.breakIndex < 0 && last > state.supply.startIndex && high > state.supply.bottom {
		return "supply_retest"
	}
	if state.demand.ok && state.demand.breakIndex < 0 && last > state.demand.startIndex && low < state.demand.top {
		return "demand_retest"
	}
	return "none"
}

func momentumSupplyDemandBreak(last int, state momentumSupplyDemandState) string {
	if state.supply.ok && state.supply.breakIndex == last {
		return "supply_break"
	}
	if state.demand.ok && state.demand.breakIndex == last {
		return "demand_break"
	}
	return "none"
}

func detectLiquiditySweep(highs []float64, lows []float64, closes []float64, pivotHighs []priceLevel, pivotLows []priceLevel) (liquiditySweepState, bool) {
	if len(closes) == 0 || len(highs) != len(closes) || len(lows) != len(closes) {
		return liquiditySweepState{}, false
	}
	best := liquiditySweepState{age: len(closes) + 1}
	found := false
	for _, pivot := range pivotHighs {
		broken := false
		for index := pivot.recency + 1; index < len(closes); index++ {
			if !broken {
				if highs[index] > pivot.price && closes[index] < pivot.price {
					best, found = newerLiquiditySweep(best, found, liquiditySweepState{
						kind:   "wick_high",
						level:  pivot.price,
						top:    highs[index],
						bottom: pivot.price,
						age:    len(closes) - 1 - index,
					})
				}
				if closes[index] > pivot.price {
					broken = true
				}
				continue
			}
			if lows[index] < pivot.price && closes[index] > pivot.price {
				best, found = newerLiquiditySweep(best, found, liquiditySweepState{
					kind:   "retest_high",
					level:  pivot.price,
					top:    pivot.price,
					bottom: lows[index],
					age:    len(closes) - 1 - index,
				})
			}
			if closes[index] < pivot.price {
				broken = false
			}
		}
	}
	for _, pivot := range pivotLows {
		broken := false
		for index := pivot.recency + 1; index < len(closes); index++ {
			if !broken {
				if lows[index] < pivot.price && closes[index] > pivot.price {
					best, found = newerLiquiditySweep(best, found, liquiditySweepState{
						kind:   "wick_low",
						level:  pivot.price,
						top:    pivot.price,
						bottom: lows[index],
						age:    len(closes) - 1 - index,
					})
				}
				if closes[index] < pivot.price {
					broken = true
				}
				continue
			}
			if highs[index] > pivot.price && closes[index] < pivot.price {
				best, found = newerLiquiditySweep(best, found, liquiditySweepState{
					kind:   "retest_low",
					level:  pivot.price,
					top:    highs[index],
					bottom: pivot.price,
					age:    len(closes) - 1 - index,
				})
			}
			if closes[index] > pivot.price {
				broken = false
			}
		}
	}
	return best, found
}

func newerLiquiditySweep(current liquiditySweepState, found bool, candidate liquiditySweepState) (liquiditySweepState, bool) {
	if !found || candidate.age < current.age {
		return candidate, true
	}
	return current, found
}

func liquiditySweepIsHigh(kind string) bool {
	return kind == "wick_high" || kind == "retest_high"
}

func liquiditySweepIsLow(kind string) bool {
	return kind == "wick_low" || kind == "retest_low"
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

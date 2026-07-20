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
	addEqualHighLowWithPivots(target, values, signals, highs, lows, closes, period, pivotHighs, pivotLows)
	addFairValueGap(target, values, signals, highs, lows, closes)
	addPremiumDiscountZones(target, values, signals, closes[last], swingHigh.price, swingLow.price)
}

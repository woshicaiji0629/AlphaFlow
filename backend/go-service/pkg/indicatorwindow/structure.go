package indicatorwindow

func addStructureWindowAnalysis(ctx *analysisContext) {
	ctx.addNumeric(
		"support_1", "support_2", "resistance_1", "resistance_2",
		"support_strength", "resistance_strength",
		"support_distance_pct", "resistance_distance_pct",
		"fib_236", "fib_382", "fib_5", "fib_618", "fib_786",
		"pivot_point", "pivot_r1", "pivot_r2", "pivot_s1", "pivot_s2",
		"ichimoku_tenkan", "ichimoku_kijun", "ichimoku_span_a", "ichimoku_span_b",
		"swing_high", "swing_low", "swing_high_distance_pct", "swing_low_distance_pct",
		"internal_swing_high", "internal_swing_low",
		"internal_swing_high_distance_pct", "internal_swing_low_distance_pct",
		"order_block_high", "order_block_low", "order_block_mid",
		"momentum_supply_top", "momentum_supply_bottom", "momentum_supply_mid", "momentum_supply_age",
		"momentum_demand_top", "momentum_demand_bottom", "momentum_demand_mid", "momentum_demand_age",
		"liquidity_sweep_level", "liquidity_sweep_top", "liquidity_sweep_bottom", "liquidity_sweep_age",
		"equal_high", "equal_low", "equal_high_distance_pct", "equal_low_distance_pct",
		"fvg_top", "fvg_bottom", "fvg_mid", "fvg_distance_pct",
		"premium_level", "discount_level", "equilibrium_level",
	)
	ctx.addSignals(
		"sr_position", "fib_zone", "pivot_zone",
		"ichimoku_trend", "ichimoku_cloud", "ichimoku_cross",
		"choch", "market_structure", "smart_money", "structure_event", "structure_bias",
		"swing_high_strength", "swing_low_strength",
		"internal_swing_high_strength", "internal_swing_low_strength",
		"momentum_sd_position", "momentum_sd_retest", "momentum_sd_break",
		"liquidity_sweep_type", "internal_structure_event", "internal_structure_bias",
		"equal_high_low", "fvg_direction", "fvg_position", "premium_discount_zone",
	)
	addStructureSemanticAnalysis(ctx)
	addSMCSemanticAnalysis(ctx)
}

func addStructureSemanticAnalysis(ctx *analysisContext) {
	bias := "neutral"
	if value, ok := latestSignal(ctx, "structure_bias"); ok {
		bias = directionBias(value)
	}
	if bias == "neutral" {
		if value, ok := latestSignal(ctx, "market_structure"); ok {
			bias = directionBias(value)
		}
	}

	event := "none"
	reversalRisk := false
	if value, ok := latestSignal(ctx, "structure_event"); ok {
		event = normalizeSignal(value)
	}
	if value, ok := latestSignal(ctx, "choch"); ok && !signalIs(value, "none", "neutral", "no") {
		event = normalizeSignal(value)
		reversalRisk = true
	}

	if support, ok := latestNumeric(ctx, "support_distance_pct"); ok && support <= structureNearLevelPct {
		reversalRisk = reversalRisk || bias == "bear"
	}
	if resistance, ok := latestNumeric(ctx, "resistance_distance_pct"); ok && resistance <= structureNearLevelPct {
		reversalRisk = reversalRisk || bias == "bull"
	}

	ctx.signals["structure_window_bias"] = bias
	ctx.signals["structure_window_event"] = event
	ctx.signals["structure_window_reversal_risk"] = boolSignal(reversalRisk)
}

func addSMCSemanticAnalysis(ctx *analysisContext) {
	bias := "neutral"
	if value, ok := latestSignal(ctx, "structure_bias"); ok {
		bias = directionBias(value)
	}
	if bias == "neutral" {
		if value, ok := latestSignal(ctx, "internal_structure_bias"); ok {
			bias = directionBias(value)
		}
	}

	event := "none"
	if value, ok := latestSignal(ctx, "structure_event"); ok {
		event = normalizeSignal(value)
	}
	if signalIs(event, "none") {
		if value, ok := latestSignal(ctx, "internal_structure_event"); ok {
			event = normalizeSignal(value)
		}
	}

	chochRecent := recentSignalContains(ctx, "structure_event", "choch") ||
		recentSignalContains(ctx, "internal_structure_event", "choch") ||
		signalStableCountFor(ctx, "choch") > 0
	bosRecent := recentSignalContains(ctx, "structure_event", "bos") ||
		recentSignalContains(ctx, "internal_structure_event", "bos")
	liquiditySweep := recentSignalContains(ctx, "structure_event", "sweep") ||
		recentSignalContains(ctx, "smart_money", "liquidity_sweep") ||
		recentSignalContains(ctx, "liquidity_sweep_type", "wick") ||
		recentSignalContains(ctx, "liquidity_sweep_type", "retest")
	momentumSDRetest := recentSignalContains(ctx, "momentum_sd_retest", "retest")
	momentumSDBreak := recentSignalContains(ctx, "momentum_sd_break", "break")
	eventAge := latestSMCEventAge(ctx)
	chochAge := latestSignalContainsAge(ctx, "structure_event", "choch")
	if internalAge := latestSignalContainsAge(ctx, "internal_structure_event", "choch"); internalAge >= 0 &&
		(chochAge < 0 || internalAge < chochAge) {
		chochAge = internalAge
	}
	bosAge := latestSignalContainsAge(ctx, "structure_event", "bos")
	if internalAge := latestSignalContainsAge(ctx, "internal_structure_event", "bos"); internalAge >= 0 &&
		(bosAge < 0 || internalAge < bosAge) {
		bosAge = internalAge
	}
	sweepAge := latestSignalContainsAge(ctx, "structure_event", "sweep")
	if smartMoneyAge := latestSignalContainsAge(ctx, "smart_money", "liquidity_sweep"); smartMoneyAge >= 0 &&
		(sweepAge < 0 || smartMoneyAge < sweepAge) {
		sweepAge = smartMoneyAge
	}
	for _, candidate := range []int{
		latestSignalContainsAge(ctx, "liquidity_sweep_type", "wick"),
		latestSignalContainsAge(ctx, "liquidity_sweep_type", "retest"),
	} {
		if candidate >= 0 && (sweepAge < 0 || candidate < sweepAge) {
			sweepAge = candidate
		}
	}

	orderBlockPosition := "unknown"
	if high, okHigh := latestNumeric(ctx, "order_block_high"); okHigh {
		if low, okLow := latestNumeric(ctx, "order_block_low"); okLow {
			orderBlockPosition = priceBandPosition(ctx, high, low)
		}
	}

	fvgPosition := "none"
	if value, ok := latestSignal(ctx, "fvg_position"); ok {
		fvgPosition = normalizeSignal(value)
	}
	zone := "unknown"
	if value, ok := latestSignal(ctx, "premium_discount_zone"); ok {
		zone = normalizeSignal(value)
	}

	reversalRisk := chochRecent || liquiditySweep || momentumSDRetest || momentumSDBreak
	if bias == "bull" && signalIs(zone, "premium") {
		reversalRisk = true
	}
	if bias == "bear" && signalIs(zone, "discount") {
		reversalRisk = true
	}
	if signalIs(fvgPosition, "inside") {
		reversalRisk = true
	}

	ctx.signals["smc_window_bias"] = bias
	ctx.signals["smc_window_event"] = event
	ctx.signals["smc_window_liquidity_sweep"] = boolSignal(liquiditySweep)
	ctx.signals["smc_window_choch_recent"] = boolSignal(chochRecent)
	ctx.signals["smc_window_bos_recent"] = boolSignal(bosRecent)
	ctx.signals["smc_window_order_block_position"] = orderBlockPosition
	ctx.signals["smc_window_fvg_position"] = fvgPosition
	ctx.signals["smc_window_premium_discount_zone"] = zone
	ctx.signals["smc_window_reversal_risk"] = boolSignal(reversalRisk)
	setInt(ctx.values, "smc_window_event_age", eventAge)
	setInt(ctx.values, "smc_window_choch_age", chochAge)
	setInt(ctx.values, "smc_window_bos_age", bosAge)
	setInt(ctx.values, "smc_window_sweep_age", sweepAge)
}

func recentSignalContains(ctx *analysisContext, key string, part string) bool {
	series := signalSeries(ctx.points, key)
	if len(series) == 0 {
		return false
	}
	start := len(series) - 3
	if start < 0 {
		start = 0
	}
	for _, value := range series[start:] {
		if signalContains(value, part) {
			return true
		}
	}
	return false
}

func latestSMCEventAge(ctx *analysisContext) int {
	age := latestSignalContainsAge(ctx, "structure_event", "bos")
	for _, candidate := range []int{
		latestSignalContainsAge(ctx, "structure_event", "choch"),
		latestSignalContainsAge(ctx, "structure_event", "sweep"),
		latestSignalContainsAge(ctx, "internal_structure_event", "bos"),
		latestSignalContainsAge(ctx, "internal_structure_event", "choch"),
		latestSignalContainsAge(ctx, "internal_structure_event", "sweep"),
		latestSignalContainsAge(ctx, "smart_money", "liquidity_sweep"),
		latestSignalContainsAge(ctx, "liquidity_sweep_type", "wick"),
		latestSignalContainsAge(ctx, "liquidity_sweep_type", "retest"),
		latestSignalContainsAge(ctx, "momentum_sd_retest", "retest"),
		latestSignalContainsAge(ctx, "momentum_sd_break", "break"),
	} {
		if candidate >= 0 && (age < 0 || candidate < age) {
			age = candidate
		}
	}
	return age
}

func latestSignalContainsAge(ctx *analysisContext, key string, part string) int {
	series := signalSeries(ctx.points, key)
	for index := len(series) - 1; index >= 0; index-- {
		if signalContains(series[index], part) {
			return len(series) - 1 - index
		}
	}
	return -1
}

func priceBandPosition(ctx *analysisContext, top float64, bottom float64) string {
	last, ok := latestNumeric(ctx, "close")
	if !ok {
		return "unknown"
	}
	switch {
	case last > top:
		return "above"
	case last < bottom:
		return "below"
	default:
		return "inside"
	}
}

package indicatorwindow

func addMovingAverageWindowAnalysis(ctx *analysisContext) {
	ctx.addNumeric(
		"sma7", "sma25", "sma99",
		"ema7", "ema25", "ema99",
		"wma7", "wma25", "wma99",
		"hma21", "hma21_slope3_pct",
		"vwma20", "kama10", "dema21", "tema21",
		"alligator_jaw", "alligator_teeth", "alligator_lips", "alligator_spread_pct",
		"ma_group_spread_pct", "ma_trend_strength",
		"ema_spread_pct", "ema25_slope5_pct",
		"price_ema7_distance_pct", "price_ema25_distance_pct", "price_ema99_distance_pct",
		"ez_ema_5", "ez_ema_8", "ez_ema_9", "ez_ema_34",
		"ez_ema_55", "ez_ema_89", "ez_ema_144", "ez_ema_200",
		"ez_ema_fast", "ez_ema_slow", "ez_ema_spread_pct", "ez_ema_group_spread_pct",
		"script_dual_ma_out1", "script_dual_ma_out2",
		"script_dual_ma_out1_slope_pct", "script_dual_ma_out2_slope_pct",
		"script_ma_breakout_pct", "script_ma_mid_direction",
		"emd_avg", "emd_value", "emd_upper", "emd_lower",
	)
	ctx.addSignals(
		"ema_alignment",
		"ma_state", "ma_arrangement", "ma_cross", "ma_spread_state",
		"ma_compression", "ma_slope_state", "ma_breakout",
		"alligator_direction", "alligator_state",
		"ez_ema_cross", "ez_price_cross_ema_pair",
		"ez_price_above_ema_pair", "ez_price_below_ema_pair",
		"ez_ema_stack", "ez_ema_spread_state", "ez_ema_compression",
		"script_dual_ma_cross", "script_ma1_direction",
		"script_price_cross_ma1", "script_price_cross_ma2", "script_ma_signal",
		"emd_direction", "emd_cross",
	)
	addMovingAverageSemanticAnalysis(ctx)
	addEZEMASemanticAnalysis(ctx)
}

func addMovingAverageSemanticAnalysis(ctx *analysisContext) {
	alignment, _ := latestSignal(ctx, "ema_alignment")
	bias := directionBias(alignment)
	if bias == "neutral" {
		if value, ok := latestSignal(ctx, "ma_arrangement"); ok {
			bias = directionBias(value)
		}
	}

	spreadState := "flat"
	if stats, ok := numericStatsFor(ctx, "ema_spread_pct"); ok {
		spreadState = stats.direction
		ctx.values["ma_window_spread_pct"] = format(stats.latest)
	}
	if spreadState == "flat" {
		if value, ok := latestSignal(ctx, "ma_spread_state"); ok {
			spreadState = normalizeSignal(value)
		}
	}

	slope := 0.0
	if value, ok := latestNumeric(ctx, "ema25_slope5_pct"); ok {
		slope = value
	} else if value, ok := latestNumeric(ctx, "hma21_slope3_pct"); ok {
		slope = value
	}
	slopeState := slopeLevel(slope)

	crossEvent := "none"
	crossAge := -1
	if event, age, ok := latestEventAge(
		ctx,
		"ma_cross",
		"golden_cross", "dead_cross", "bull_cross", "bear_cross",
	); ok {
		crossEvent = event
		crossAge = age
	} else if event, age, ok := latestEventAge(
		ctx,
		"script_dual_ma_cross",
		"golden_cross", "dead_cross", "bull_cross", "bear_cross",
	); ok {
		crossEvent = event
		crossAge = age
	}
	setInt(ctx.values, "ma_window_cross_age", crossAge)

	crossFlips := signalChangeCount(ctx, "ma_cross") + signalChangeCount(ctx, "script_dual_ma_cross")
	tangled := crossFlips >= choppyCrossFlipCount || signalIs(spreadState, "compressed", "tight", "contracting")
	if spread, ok := latestNumeric(ctx, "ema_spread_pct"); ok && spread < maTangleSpreadPct {
		tangled = true
	}
	if value, ok := latestSignal(ctx, "ma_compression"); ok && signalIs(value, "true", "yes", "compressed", "tight") {
		tangled = true
	}

	phase := "neutral"
	switch {
	case tangled:
		phase = "choppy"
	case crossAge >= 0 && crossAge <= earlyCrossBars:
		phase = "early_cross"
	case bias != "neutral" && signalIs(spreadState, "rising", "expanding") && slopeState != "flat":
		phase = "spreading"
	case bias != "neutral" && signalIs(spreadState, "falling", "contracting"):
		phase = "weakening"
	case bias != "neutral":
		phase = "trend"
	}

	ctx.signals["ma_window_bias"] = bias
	ctx.signals["ma_window_phase"] = phase
	ctx.signals["ma_window_tangled"] = boolSignal(tangled)
	ctx.signals["ma_window_slope_level"] = slopeState
	ctx.signals["ma_window_spread_state"] = spreadState
	ctx.signals["ma_window_cross_event"] = crossEvent
	ctx.signals["ma_window_cross_recent"] = boolSignal(crossAge >= 0 && crossAge <= recentEventBars)
	addMARibbonSemantic(ctx, "ma_ribbon", bias, phase, spreadState, slopeState, tangled, crossAge)
}

func addEZEMASemanticAnalysis(ctx *analysisContext) {
	stack, _ := latestSignal(ctx, "ez_ema_stack")
	bias := directionBias(stack)

	spreadState := "flat"
	if stats, ok := numericStatsFor(ctx, "ez_ema_group_spread_pct"); ok {
		spreadState = stats.direction
		ctx.values["ez_ema_window_group_spread_pct"] = format(stats.latest)
	} else if stats, ok := numericStatsFor(ctx, "ez_ema_spread_pct"); ok {
		spreadState = stats.direction
		ctx.values["ez_ema_window_spread_pct"] = format(stats.latest)
	}
	if value, ok := latestSignal(ctx, "ez_ema_spread_state"); ok && spreadState == "flat" {
		spreadState = normalizeSignal(value)
	}

	slopeState := "flat"
	if stats, ok := numericStatsFor(ctx, "ez_ema_fast"); ok {
		slopeState = slopeLevel(stats.slope)
	}

	crossEvent := "none"
	crossAge := -1
	if event, age, ok := latestEventAge(
		ctx,
		"ez_ema_cross",
		"golden", "dead", "golden_cross", "dead_cross",
	); ok {
		crossEvent = event
		crossAge = age
	}
	setInt(ctx.values, "ez_ema_window_cross_age", crossAge)

	priceCross := "none"
	if value, ok := latestSignal(ctx, "ez_price_cross_ema_pair"); ok {
		priceCross = normalizeSignal(value)
	}

	crossFlips := signalChangeCount(ctx, "ez_ema_cross")
	tangled := crossFlips >= choppyCrossFlipCount ||
		signalIs(spreadState, "compressed", "tight", "contracting")
	if value, ok := latestSignal(ctx, "ez_ema_compression"); ok &&
		signalIs(value, "compressed", "true", "yes") {
		tangled = true
	}

	phase := "neutral"
	switch {
	case tangled:
		phase = "choppy"
	case crossAge >= 0 && crossAge <= earlyCrossBars:
		phase = "early_cross"
	case bias != "neutral" && signalIs(spreadState, "rising", "expanding") && slopeState != "flat":
		phase = "spreading"
	case bias != "neutral" && signalIs(spreadState, "falling", "contracting"):
		phase = "weakening"
	case bias != "neutral":
		phase = "trend"
	}

	ctx.signals["ez_ema_window_bias"] = bias
	ctx.signals["ez_ema_window_phase"] = phase
	ctx.signals["ez_ema_window_cross_event"] = crossEvent
	ctx.signals["ez_ema_window_cross_recent"] = boolSignal(crossAge >= 0 && crossAge <= recentEventBars)
	ctx.signals["ez_ema_window_price_cross_pair"] = priceCross
	ctx.signals["ez_ema_window_tangled"] = boolSignal(tangled)
	ctx.signals["ez_ema_window_spread_state"] = spreadState
	ctx.signals["ez_ema_window_slope_level"] = slopeState
	addMARibbonSemantic(ctx, "ez_ema_ribbon", bias, phase, spreadState, slopeState, tangled, crossAge)
}

func addMARibbonSemantic(
	ctx *analysisContext,
	prefix string,
	bias string,
	phase string,
	spreadState string,
	slopeState string,
	tangled bool,
	crossAge int,
) {
	state := "neutral"
	switch {
	case tangled:
		state = "tangled"
	case bias == "bull" && signalIs(spreadState, "rising", "expanding"):
		state = "bullish_fan"
	case bias == "bear" && signalIs(spreadState, "rising", "expanding"):
		state = "bearish_fan"
	case signalIs(spreadState, "falling", "contracting", "compressed", "tight"):
		state = "compressing"
	case bias != "neutral":
		state = "expanding"
	}

	ribbonPhase := "base"
	switch {
	case tangled:
		ribbonPhase = "base"
	case crossAge >= 0 && crossAge <= earlyCrossBars:
		ribbonPhase = "early_expand"
	case signalIs(state, "bullish_fan", "bearish_fan") && signalContains(slopeState, "steep"):
		ribbonPhase = "trend"
	case signalIs(state, "compressing") && bias != "neutral":
		ribbonPhase = "late"
	case phase != "neutral":
		ribbonPhase = phase
	}

	ctx.signals[prefix+"_state"] = state
	ctx.signals[prefix+"_phase"] = ribbonPhase
	ctx.signals[prefix+"_pullback"] = maRibbonPullback(ctx, bias)
}

func maRibbonPullback(ctx *analysisContext, bias string) string {
	closeValue, closeOK := latestNumeric(ctx, "close")
	fast, fastOK := latestNumeric(ctx, "ema7")
	mid, midOK := latestNumeric(ctx, "ema25")
	slow, slowOK := latestNumeric(ctx, "ema99")
	if !closeOK || !fastOK {
		return "unknown"
	}
	if !midOK || !slowOK {
		if closeValue >= fast {
			return "above"
		}
		return "below"
	}

	upper := fast
	lower := slow
	if lower > upper {
		upper, lower = lower, upper
	}
	switch {
	case bias == "bull" && closeValue >= upper:
		return "above"
	case bias == "bear" && closeValue <= lower:
		return "below"
	case closeValue <= upper && closeValue >= lower:
		if bias != "neutral" && mid >= lower && mid <= upper {
			return "retest"
		}
		return "inside"
	case closeValue > upper:
		return "above"
	case closeValue < lower:
		return "below"
	default:
		return "inside"
	}
}

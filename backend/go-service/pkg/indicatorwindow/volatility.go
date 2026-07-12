package indicatorwindow

func addVolatilityWindowAnalysis(ctx *analysisContext) {
	ctx.addNumeric(
		"atr14", "atr_pct14", "natr14",
		"bb_upper", "bb_middle", "bb_lower",
		"bb_width_pct", "bb_percent_b", "bb_width_delta",
		"bb_middle_slope_pct", "bb_upper_slope_pct", "bb_lower_slope_pct",
		"donchian_high20", "donchian_low20",
		"supertrend", "supertrend_distance_pct", "supertrend_stop_distance_pct",
		"supertrend_7_2", "supertrend_10_3", "supertrend_10_3_3", "supertrend_14_4",
		"adaptive_supertrend", "adaptive_supertrend_distance_pct", "adaptive_supertrend_assigned_atr",
		"adaptive_supertrend_high_centroid", "adaptive_supertrend_mid_centroid", "adaptive_supertrend_low_centroid",
		"ai_supertrend", "ai_supertrend_ama", "ai_supertrend_distance_pct", "ai_supertrend_target_factor",
		"ai_supertrend_performance_index", "ai_supertrend_best_centroid", "ai_supertrend_average_centroid", "ai_supertrend_worst_centroid",
		"ai_source_ma", "ai_source_value", "ai_source_drive",
		"ai_source_score_open", "ai_source_score_high", "ai_source_score_low", "ai_source_score_close",
		"ai_source_supertrend", "ai_source_supertrend_distance_pct", "ai_source_supertrend_adapt_mult",
		"alphatrend", "alphatrend_distance_pct", "alphatrend_slope_pct",
		"psar", "psar_distance_pct",
		"chandelier_long", "chandelier_short", "chandelier_stop_distance_pct",
		"squeeze_momentum", "squeeze_momentum_delta",
		"livermore_active_point",
	)
	ctx.addSignals(
		"volatility_state", "trend_direction",
		"bb_position", "bb_width_state", "bb_trend",
		"supertrend_direction", "supertrend_flip",
		"supertrend_7_2_direction", "supertrend_10_3_direction",
		"supertrend_10_3_3_direction", "supertrend_14_4_direction",
		"adaptive_supertrend_direction", "adaptive_supertrend_flip", "adaptive_supertrend_volatility_cluster",
		"ai_supertrend_direction", "ai_supertrend_flip", "ai_supertrend_cluster", "ai_supertrend_factor_cluster",
		"ai_source_selected", "ai_source_changed", "ai_source_supertrend_direction", "ai_source_supertrend_flip", "ai_source_ready",
		"alphatrend_direction", "alphatrend_flip", "alphatrend_cross", "alphatrend_signal",
		"psar_direction", "chandelier_direction",
		"squeeze", "squeeze_state", "momentum_state",
		"livermore_trend", "livermore_signal",
	)
	addTrendVolatilitySemanticAnalysis(ctx)
}

func addTrendVolatilitySemanticAnalysis(ctx *analysisContext) {
	supertrend, _ := latestSignal(ctx, "supertrend_direction")
	alphatrend, _ := latestSignal(ctx, "alphatrend_direction")
	supertrendBias := directionBias(supertrend)
	alphatrendBias := directionBias(alphatrend)

	bias := supertrendBias
	if bias == "neutral" {
		bias = alphatrendBias
	}
	if supertrendBias != "neutral" && alphatrendBias != "neutral" && supertrendBias != alphatrendBias {
		bias = "neutral"
	}

	flipCount := signalChangeCount(ctx, "supertrend_direction") +
		signalChangeCount(ctx, "alphatrend_direction")
	setInt(ctx.values, "trend_window_flip_count", flipCount)

	stableCount := signalStableCountFor(ctx, "supertrend_direction")
	if alphaStable := signalStableCountFor(ctx, "alphatrend_direction"); alphaStable > stableCount {
		stableCount = alphaStable
	}
	setInt(ctx.values, "trend_window_stable_count", stableCount)

	flipRecent := false
	if _, age, ok := latestEventAge(ctx, "supertrend_flip", "bull", "bear", "up", "down", "buy", "sell"); ok {
		flipRecent = age <= recentTrendFlipBars
	}
	if _, age, ok := latestEventAge(ctx, "alphatrend_flip", "bull", "bear", "up", "down", "buy", "sell"); ok {
		flipRecent = flipRecent || age <= recentTrendFlipBars
	}
	signalEvent, signalAge := trendSignalEvent(ctx, bias)
	setInt(ctx.values, "trend_signal_age", signalAge)

	reversalRisk := flipCount >= 2 || flipRecent
	distanceState := "flat"
	if stats, ok := numericStatsFor(ctx, "supertrend_distance_pct"); ok {
		distanceState = stats.direction
		reversalRisk = reversalRisk ||
			(bias == "bull" && stats.direction == "falling") ||
			(bias == "bear" && stats.direction == "rising")
	} else if stats, ok := numericStatsFor(ctx, "alphatrend_distance_pct"); ok {
		distanceState = stats.direction
		reversalRisk = reversalRisk ||
			(bias == "bull" && stats.direction == "falling") ||
			(bias == "bear" && stats.direction == "rising")
	}

	priceProgress := trendPriceProgress(ctx, bias)
	volatilityState := "neutral"
	if value, ok := latestSignal(ctx, "volatility_state"); ok {
		volatilityState = normalizeSignal(value)
	} else if value, ok := latestSignal(ctx, "bb_width_state"); ok {
		volatilityState = normalizeSignal(value)
	}

	choppyContext := signalIs(ctx.signals["ma_window_phase"], "choppy") ||
		signalIs(ctx.signals["ez_ema_window_phase"], "choppy") ||
		signalIs(volatilityState, "contracting", "squeeze", "low")
	confirmedContext := signalIs(ctx.signals["macd_window_confirmed"], "true") ||
		signalIs(ctx.signals["ma_window_phase"], "spreading", "trend", "early_cross") ||
		signalIs(ctx.signals["ez_ema_window_phase"], "spreading", "trend", "early_cross")

	continuation := bias != "neutral" &&
		stableCount >= minContinuationBars &&
		!reversalRisk &&
		priceProgress == "advancing" &&
		!choppyContext

	breakoutQuality := "neutral"
	if value, ok := latestSignal(ctx, "squeeze_state"); ok && signalContains(value, "release", "off") {
		breakoutQuality = "improving"
	}
	if value, ok := latestSignal(ctx, "breakout_volume_confirm"); ok && directionBias(value) != "neutral" {
		breakoutQuality = "confirmed"
	}

	quality := "weak"
	if bias == "neutral" || choppyContext {
		quality = "choppy"
	} else if continuation && confirmedContext && signalIs(distanceState, "rising") {
		quality = "strong"
	}
	fakeRisk := "medium"
	if quality == "strong" {
		fakeRisk = "low"
	}
	if quality == "choppy" || priceProgress == "stalling" || reversalRisk {
		fakeRisk = "high"
	}
	valid := quality == "strong" || (quality == "weak" && continuation && confirmedContext)

	ctx.signals["trend_signal_event"] = signalEvent
	ctx.signals["trend_signal_recent"] = boolSignal(signalAge >= 0 && signalAge <= recentEventBars)
	ctx.signals["trend_window_bias"] = bias
	ctx.signals["trend_window_continuation"] = boolSignal(continuation)
	ctx.signals["trend_window_flip_recent"] = boolSignal(flipRecent)
	ctx.signals["trend_window_reversal_risk"] = boolSignal(reversalRisk)
	ctx.signals["trend_distance_state"] = distanceState
	ctx.signals["trend_price_progress"] = priceProgress
	ctx.signals["trend_quality"] = quality
	ctx.signals["trend_valid"] = boolSignal(valid)
	ctx.signals["trend_fake_risk"] = fakeRisk
	ctx.signals["volatility_window_state"] = volatilityState
	ctx.signals["breakout_window_quality"] = breakoutQuality
}

func trendSignalEvent(ctx *analysisContext, currentBias string) (string, int) {
	if event, age, ok := latestEventAge(ctx, "supertrend_flip", "bull", "bear", "up", "down", "buy", "sell"); ok {
		return trendEventName(event), age
	}
	if event, age, ok := latestEventAge(ctx, "alphatrend_signal", "buy", "sell"); ok {
		return trendEventName(event), age
	}
	if event, age, ok := latestEventAge(ctx, "alphatrend_flip", "bull", "bear", "up", "down", "buy", "sell"); ok {
		return trendEventName(event), age
	}
	if event, age, ok := latestDirectionChangeEvent(ctx, "supertrend_direction"); ok {
		return event, age
	}
	if event, age, ok := latestDirectionChangeEvent(ctx, "alphatrend_direction"); ok {
		return event, age
	}
	if currentBias == "neutral" {
		return "none", -1
	}
	return "none", -1
}

func trendEventName(event string) string {
	switch {
	case signalIs(event, "bull", "up", "buy"):
		return "buy"
	case signalIs(event, "bear", "down", "sell"):
		return "sell"
	default:
		return "none"
	}
}

func latestDirectionChangeEvent(ctx *analysisContext, key string) (string, int, bool) {
	current := ""
	age := 0
	for index := len(ctx.points) - 1; index >= 0; index-- {
		value, ok := ctx.points[index].signals[key]
		if !ok {
			continue
		}
		if current == "" {
			current = directionBias(value)
			continue
		}
		previous := directionBias(value)
		if current == "neutral" || current == previous {
			current = previous
			age++
			continue
		}
		if current == "bull" {
			return "buy", age, true
		}
		return "sell", age, true
	}
	return "", 0, false
}

func trendPriceProgress(ctx *analysisContext, bias string) string {
	stats, ok := numericStatsFor(ctx, "close")
	if !ok || bias == "neutral" {
		return "unknown"
	}
	switch {
	case bias == "bull" && stats.direction == "rising":
		return "advancing"
	case bias == "bear" && stats.direction == "falling":
		return "advancing"
	case bias == "bull" && stats.direction == "falling":
		return "reversing"
	case bias == "bear" && stats.direction == "rising":
		return "reversing"
	default:
		return "stalling"
	}
}

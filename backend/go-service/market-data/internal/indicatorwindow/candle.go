package indicatorwindow

func addCandleWindowAnalysis(ctx *analysisContext) {
	ctx.addNumeric(
		"change_pct", "amplitude_pct", "body_ratio",
		"upper_shadow_ratio", "lower_shadow_ratio",
		"ha_open", "ha_high", "ha_low", "ha_close",
	)
	ctx.addSignals(
		"candle_pattern", "candle_bias", "candle_strength", "pin_bar",
		"ha_trend", "ha_strength",
	)
	addCandleSemanticAnalysis(ctx)
}

func addCandleSemanticAnalysis(ctx *analysisContext) {
	bias := "neutral"
	if value, ok := latestSignal(ctx, "candle_bias"); ok {
		bias = directionBias(value)
	}
	if bias == "neutral" {
		if value, ok := latestSignal(ctx, "ha_trend"); ok {
			bias = directionBias(value)
		}
	}

	strength := "normal"
	if value, ok := latestSignal(ctx, "candle_strength"); ok {
		strength = normalizeSignal(value)
	} else if bodyRatio, ok := latestNumeric(ctx, "body_ratio"); ok {
		switch {
		case bodyRatio >= candleStrongBodyRatio:
			strength = "strong"
		case bodyRatio <= candleWeakBodyRatio:
			strength = "weak"
		}
	}

	reversalRisk := false
	if value, ok := latestSignal(ctx, "pin_bar"); ok && !signalIs(value, "none", "neutral", "no") {
		reversalRisk = true
	}
	if upper, ok := latestNumeric(ctx, "upper_shadow_ratio"); ok && upper >= candleLongShadowRatio && bias == "bull" {
		reversalRisk = true
	}
	if lower, ok := latestNumeric(ctx, "lower_shadow_ratio"); ok && lower >= candleLongShadowRatio && bias == "bear" {
		reversalRisk = true
	}

	ctx.signals["candle_window_bias"] = bias
	ctx.signals["candle_window_strength"] = strength
	ctx.signals["candle_window_reversal_risk"] = boolSignal(reversalRisk)
}

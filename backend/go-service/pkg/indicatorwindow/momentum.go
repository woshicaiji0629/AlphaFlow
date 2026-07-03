package indicatorwindow

func addMomentumWindowAnalysis(ctx *analysisContext) {
	ctx.addNumeric(
		"rsi14", "rsi_slope3",
		"kdj_k", "kdj_d", "kdj_j",
		"stoch_k", "stoch_d",
		"stoch_rsi_k", "stoch_rsi_d",
		"skdj_k", "skdj_d",
		"cci20", "williams_r14", "roc12",
		"wavetrend_wt1", "wavetrend_wt2", "wavetrend_delta",
	)
	ctx.addSignals(
		"rsi_state", "rsi_divergence",
		"stoch_rsi_state", "skdj_cross",
		"cci_state", "williams_state", "roc_state",
		"wavetrend_cross", "wavetrend_zone", "wavetrend_momentum",
	)
	addMomentumSemanticAnalysis(ctx)
}

func addMomentumSemanticAnalysis(ctx *analysisContext) {
	bias := "neutral"
	level := "neutral"
	slopeState := "flat"
	overheated := false
	fading := false

	if rsiStats, ok := numericStatsFor(ctx, "rsi14"); ok {
		switch {
		case rsiStats.latest >= rsiOverbought:
			level = "overbought"
			overheated = true
		case rsiStats.latest <= rsiOversold:
			level = "oversold"
		case rsiStats.latest >= rsiBullLevel:
			bias = "bull"
		case rsiStats.latest <= rsiBearLevel:
			bias = "bear"
		}
		slopeState = slopeLevel(rsiStats.slope)
		fading = (bias == "bull" && rsiStats.direction == "falling") ||
			(bias == "bear" && rsiStats.direction == "rising")
	}

	if value, ok := latestSignal(ctx, "wavetrend_momentum"); ok {
		waveBias := directionBias(value)
		if waveBias != "neutral" {
			bias = waveBias
		}
	}
	if value, ok := latestSignal(ctx, "wavetrend_zone"); ok &&
		signalIs(value, "overbought", "oversold") {
		level = normalizeSignal(value)
		overheated = signalIs(value, "overbought")
	}
	if value, ok := latestSignal(ctx, "rsi_divergence"); ok &&
		!signalIs(value, "none", "neutral", "no") {
		fading = true
	}

	ctx.signals["momentum_window_bias"] = bias
	ctx.signals["momentum_window_level"] = level
	ctx.signals["momentum_window_slope_level"] = slopeState
	ctx.signals["momentum_window_overheated"] = boolSignal(overheated)
	ctx.signals["momentum_window_fading"] = boolSignal(fading)
}

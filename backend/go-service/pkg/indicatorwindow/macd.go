package indicatorwindow

func addMACDWindowAnalysis(ctx *analysisContext) {
	ctx.addNumeric(
		"macd", "macd_signal", "macd_hist", "macd_hist_delta", "macd_zero_distance",
		"macd_fast", "macd_fast_signal", "macd_fast_hist",
		"macd_fast_hist_delta", "macd_fast_zero_distance",
	)
	ctx.addSignals(
		"macd_cross", "macd_zone", "macd_momentum", "macd_hist_phase", "macd_signal_side", "macd_divergence",
		"macd_fast_cross", "macd_fast_zone", "macd_fast_momentum", "macd_fast_hist_phase", "macd_fast_signal_side", "macd_fast_divergence",
	)
	addMACDSemanticAnalysis(ctx)
}

func addMACDSemanticAnalysis(ctx *analysisContext) {
	histStats, hasHist := numericStatsFor(ctx, "macd_hist")
	if !hasHist {
		histStats, hasHist = numericStatsFor(ctx, "macd_fast_hist")
	}

	bias := "neutral"
	confirmed := false
	acceleration := "flat"
	strength := "weak"
	reversalRisk := false
	zeroSide := "unknown"
	quality := "neutral"

	if hasHist {
		switch {
		case histStats.latest > 0:
			bias = "bull"
		case histStats.latest < 0:
			bias = "bear"
		}
		acceleration = histStats.direction
		if histStats.risingCount >= 2 || histStats.fallingCount >= 2 {
			strength = "strong"
		}
		confirmed = (bias == "bull" && histStats.direction == "rising") ||
			(bias == "bear" && histStats.direction == "falling")
		reversalRisk = (bias == "bull" && histStats.direction == "falling") ||
			(bias == "bear" && histStats.direction == "rising")
	}

	if value, ok := latestSignal(ctx, "macd_zone"); ok {
		zoneBias := directionBias(value)
		if zoneBias != "neutral" {
			bias = zoneBias
		}
	}
	if value, ok := latestSignal(ctx, "macd_divergence"); ok &&
		!signalIs(value, "none", "neutral", "no") {
		reversalRisk = true
	}
	if zeroDistance, ok := latestNumeric(ctx, "macd_zero_distance"); ok {
		zeroSide = macdZeroSide(zeroDistance)
	} else if zeroDistance, ok := latestNumeric(ctx, "macd_fast_zero_distance"); ok {
		zeroSide = macdZeroSide(zeroDistance)
	}
	switch {
	case bias == "bull" && zeroSide == "above" && confirmed && strength == "strong":
		quality = "strong"
	case bias == "bear" && zeroSide == "below" && confirmed && strength == "strong":
		quality = "strong"
	case confirmed:
		quality = "weak"
	}

	ctx.signals["macd_window_bias"] = bias
	ctx.signals["macd_window_confirmed"] = boolSignal(confirmed)
	ctx.signals["macd_window_acceleration"] = acceleration
	ctx.signals["macd_window_strength"] = strength
	ctx.signals["macd_window_zero_side"] = zeroSide
	ctx.signals["macd_window_quality"] = quality
	ctx.signals["macd_window_reversal_risk"] = boolSignal(reversalRisk)
}

func macdZeroSide(distance float64) string {
	switch {
	case distance > 0.00000001:
		return "above"
	case distance < -0.00000001:
		return "below"
	default:
		return "crossing"
	}
}

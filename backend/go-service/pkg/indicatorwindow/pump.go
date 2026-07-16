package indicatorwindow

import "strconv"

func addPumpWindowAnalysis(ctx *analysisContext) {
	volumeExpansion := signalIs(ctx.signals["volume_window_state"], "expansion", "climax") ||
		signalIs(ctx.signals["volume_window_support"], "true") ||
		signalIs(ctx.signals["price_volume_window_confirmed"], "true")
	trendAdvancing := signalIs(ctx.signals["trend_price_progress"], "advancing")
	priceAdvancing := signalIs(ctx.signals["trend_window_bias"], "bull") && trendAdvancing
	priceDeclining := signalIs(ctx.signals["trend_window_bias"], "bear") && trendAdvancing
	trendOK := signalIs(ctx.signals["trend_valid"], "true") ||
		signalIs(ctx.signals["trend_quality"], "strong")
	bullMACDOK := signalIs(ctx.signals["macd_window_bias"], "bull") &&
		signalIs(ctx.signals["macd_window_quality"], "strong", "weak")
	bearMACDOK := signalIs(ctx.signals["macd_window_bias"], "bear") &&
		signalIs(ctx.signals["macd_window_quality"], "strong", "weak")
	bullBreakoutOK := signalIs(ctx.signals["volume_profile_window_breakout_quality"], "confirmed") ||
		signalIs(ctx.signals["smc_window_bos_recent"], "true") ||
		signalContains(ctx.signals["structure_window_event"], "breakout", "bos", "up")
	bearBreakoutOK := signalIs(ctx.signals["volume_profile_window_breakout_quality"], "confirmed") ||
		signalIs(ctx.signals["smc_window_bos_recent"], "true") ||
		signalContains(ctx.signals["structure_window_event"], "breakdown", "bos", "down")
	bullEarlyOK := signalIs(ctx.signals["trend_signal_recent"], "true") ||
		signalIs(ctx.signals["ma_window_phase"], "early_cross") ||
		signalIs(ctx.signals["ez_ema_window_phase"], "early_cross")
	bearEarlyOK := signalIs(ctx.signals["trend_signal_recent"], "true") ||
		signalIs(ctx.signals["ma_window_cross_event"], "dead_cross", "bear_cross") ||
		signalIs(ctx.signals["ez_ema_window_cross_event"], "dead", "dead_cross")

	fakeRiskHigh := signalIs(ctx.signals["volume_window_climax"], "true") &&
		(!priceAdvancing ||
			signalIs(ctx.signals["candle_window_reversal_risk"], "true") ||
			(signalIs(ctx.signals["volume_profile_window_near_poc"], "true") &&
				!signalIs(ctx.signals["trend_quality"], "strong")))
	fakeRiskMedium := !fakeRiskHigh && signalIs(ctx.signals["volume_profile_window_near_value_edge"], "true") &&
		!signalIs(ctx.signals["volume_profile_window_breakout_quality"], "confirmed")

	signal := priceAdvancing && volumeExpansion && (trendOK || bullEarlyOK || bullBreakoutOK)
	score := volumePushWindowScore(volumeExpansion, priceAdvancing, trendOK, bullMACDOK, bullBreakoutOK, bullEarlyOK, fakeRiskHigh, fakeRiskMedium)

	ctx.signals["pump_window_signal"] = boolSignal(signal)
	ctx.signals["pump_window_fake_risk"] = volumePushWindowFakeRisk(fakeRiskHigh, fakeRiskMedium)
	ctx.signals["pump_window_quality"] = volumePushWindowQuality(signal, trendOK, bullMACDOK, bullBreakoutOK, fakeRiskHigh, fakeRiskMedium)
	ctx.signals["pump_window_stage"] = volumePushWindowStage(signal, trendOK, bullMACDOK, bullBreakoutOK, bullEarlyOK, fakeRiskHigh)
	ctx.signals["pump_window_reason"] = pumpWindowReason(volumeExpansion, priceAdvancing, trendOK, bullMACDOK, bullBreakoutOK, bullEarlyOK, fakeRiskHigh)
	ctx.values["pump_window_score"] = strconv.Itoa(score)

	dumpFakeRiskHigh := signalIs(ctx.signals["volume_window_climax"], "true") &&
		(!priceDeclining ||
			signalIs(ctx.signals["candle_window_reversal_risk"], "true") ||
			(signalIs(ctx.signals["volume_profile_window_near_poc"], "true") &&
				!signalIs(ctx.signals["trend_quality"], "strong")))
	dumpFakeRiskMedium := !dumpFakeRiskHigh && signalIs(ctx.signals["volume_profile_window_near_value_edge"], "true") &&
		!signalIs(ctx.signals["volume_profile_window_breakout_quality"], "confirmed")
	dumpSignal := priceDeclining && volumeExpansion && (trendOK || bearEarlyOK || bearBreakoutOK)
	dumpScore := volumePushWindowScore(volumeExpansion, priceDeclining, trendOK, bearMACDOK, bearBreakoutOK, bearEarlyOK, dumpFakeRiskHigh, dumpFakeRiskMedium)

	ctx.signals["dump_window_signal"] = boolSignal(dumpSignal)
	ctx.signals["dump_window_fake_risk"] = volumePushWindowFakeRisk(dumpFakeRiskHigh, dumpFakeRiskMedium)
	ctx.signals["dump_window_quality"] = volumePushWindowQuality(dumpSignal, trendOK, bearMACDOK, bearBreakoutOK, dumpFakeRiskHigh, dumpFakeRiskMedium)
	ctx.signals["dump_window_stage"] = volumePushWindowStage(dumpSignal, trendOK, bearMACDOK, bearBreakoutOK, bearEarlyOK, dumpFakeRiskHigh)
	ctx.signals["dump_window_reason"] = dumpWindowReason(volumeExpansion, priceDeclining, trendOK, bearMACDOK, bearBreakoutOK, bearEarlyOK, dumpFakeRiskHigh)
	ctx.values["dump_window_score"] = strconv.Itoa(dumpScore)
}

func volumePushWindowScore(
	volumeExpansion bool,
	priceMoving bool,
	trendOK bool,
	macdOK bool,
	breakoutOK bool,
	earlyOK bool,
	fakeRiskHigh bool,
	fakeRiskMedium bool,
) int {
	score := 0
	if priceMoving {
		score += 20
	}
	if volumeExpansion {
		score += 20
	}
	if trendOK {
		score += 20
	}
	if macdOK {
		score += 15
	}
	if breakoutOK {
		score += 15
	}
	if earlyOK {
		score += 10
	}
	if fakeRiskHigh {
		score -= 35
	} else if fakeRiskMedium {
		score -= 15
	}
	return clampScore(score)
}

func volumePushWindowFakeRisk(fakeRiskHigh bool, fakeRiskMedium bool) string {
	switch {
	case fakeRiskHigh:
		return "high"
	case fakeRiskMedium:
		return "medium"
	default:
		return "low"
	}
}

func volumePushWindowQuality(
	signal bool,
	trendOK bool,
	macdOK bool,
	breakoutOK bool,
	fakeRiskHigh bool,
	fakeRiskMedium bool,
) string {
	switch {
	case !signal:
		return "neutral"
	case fakeRiskHigh:
		return "fake_risk"
	case trendOK && macdOK && breakoutOK && !fakeRiskMedium:
		return "strong"
	default:
		return "weak"
	}
}

func volumePushWindowStage(
	signal bool,
	trendOK bool,
	macdOK bool,
	breakoutOK bool,
	earlyOK bool,
	fakeRiskHigh bool,
) string {
	switch {
	case !signal:
		return "none"
	case fakeRiskHigh:
		return "exhausted"
	case trendOK && macdOK && breakoutOK:
		return "accelerating"
	case earlyOK:
		return "early"
	default:
		return "starting"
	}
}

func pumpWindowReason(
	volumeExpansion bool,
	priceAdvancing bool,
	trendOK bool,
	macdOK bool,
	breakoutOK bool,
	earlyOK bool,
	fakeRiskHigh bool,
) string {
	switch {
	case fakeRiskHigh:
		return "fake_volume_risk"
	case volumeExpansion && priceAdvancing && trendOK && macdOK && breakoutOK:
		return "volume_trend_macd_breakout"
	case volumeExpansion && priceAdvancing && earlyOK:
		return "early_volume_breakout"
	case volumeExpansion && priceAdvancing:
		return "price_volume_push"
	default:
		return "none"
	}
}

func dumpWindowReason(
	volumeExpansion bool,
	priceDeclining bool,
	trendOK bool,
	macdOK bool,
	breakoutOK bool,
	earlyOK bool,
	fakeRiskHigh bool,
) string {
	switch {
	case fakeRiskHigh:
		return "fake_volume_risk"
	case volumeExpansion && priceDeclining && trendOK && macdOK && breakoutOK:
		return "volume_trend_macd_breakdown"
	case volumeExpansion && priceDeclining && earlyOK:
		return "early_volume_breakdown"
	case volumeExpansion && priceDeclining:
		return "price_volume_drop"
	default:
		return "none"
	}
}

func clampScore(score int) int {
	switch {
	case score < 0:
		return 0
	case score > 100:
		return 100
	default:
		return score
	}
}

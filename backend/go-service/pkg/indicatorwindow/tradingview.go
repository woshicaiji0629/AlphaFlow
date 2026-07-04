package indicatorwindow

func addTradingViewWindowAnalysis(ctx *analysisContext) {
	ctx.addNumeric(
		"donchian_mid20", "donchian_width_pct20", "donchian_position20",
		"keltner_middle20", "keltner_width_pct20", "keltner_position20",
		"qqe_line", "qqe_signal", "qqe_hist",
		"qqe_primary_line", "qqe_primary_trend", "qqe_secondary_line", "qqe_secondary_trend",
		"qqe_bb_upper", "qqe_bb_lower", "qqe_primary_hist", "qqe_secondary_hist",
		"ut_stop", "ut_stop_distance_pct",
		"ssl_upper", "ssl_lower", "ssl_width_pct",
		"range_filter", "range_filter_upper", "range_filter_lower", "range_filter_distance_pct",
		"wvf", "wvf_mid_line", "wvf_upper_band", "wvf_lower_band", "wvf_range_high", "wvf_range_low",
		"td_buy_setup_count", "td_sell_setup_count",
		"nw_middle", "nw_upper", "nw_lower", "nw_width_pct", "nw_position",
	)
	ctx.addSignals(
		"donchian_breakout", "keltner_breakout",
		"qqe_trend", "qqe_cross", "qqe_mod_signal", "qqe_primary_zero_cross",
		"ut_direction", "ut_signal",
		"ssl_direction", "ssl_cross",
		"range_filter_direction",
		"wvf_state", "wvf_zone", "td_exhaustion",
		"nw_trend", "nw_position_state",
	)
	addChannelSemanticAnalysis(ctx)
	addTradingViewSemanticAnalysis(ctx)
}

func addChannelSemanticAnalysis(ctx *analysisContext) {
	donchianBreakout, _ := latestSignal(ctx, "donchian_breakout")
	keltnerBreakout, _ := latestSignal(ctx, "keltner_breakout")
	donchianBias := breakoutBias(donchianBreakout)
	keltnerBias := breakoutBias(keltnerBreakout)

	bias := "neutral"
	if donchianBias != "neutral" && keltnerBias != oppositeBias(donchianBias) {
		bias = donchianBias
	} else if keltnerBias != "neutral" {
		bias = keltnerBias
	}

	quality := "neutral"
	switch {
	case donchianBias != "neutral" && donchianBias == keltnerBias:
		quality = "strong"
	case donchianBias != "neutral" || keltnerBias != "neutral":
		quality = "weak"
	}

	volatilityState := "flat"
	if stats, ok := numericStatsFor(ctx, "donchian_width_pct20"); ok {
		volatilityState = channelWidthState(stats.direction)
	}
	if stats, ok := numericStatsFor(ctx, "keltner_width_pct20"); ok && volatilityState == "flat" {
		volatilityState = channelWidthState(stats.direction)
	}

	positionState := "middle"
	if position, ok := latestNumeric(ctx, "keltner_position20"); ok {
		positionState = channelPositionState(position)
	} else if position, ok := latestNumeric(ctx, "donchian_position20"); ok {
		positionState = channelPositionState(position)
	}

	fakeRisk := "medium"
	if quality == "strong" && volatilityState == "expanding" {
		fakeRisk = "low"
	}
	if quality == "weak" || volatilityState == "contracting" {
		fakeRisk = "high"
	}

	ctx.signals["channel_window_bias"] = bias
	ctx.signals["channel_breakout_quality"] = quality
	ctx.signals["channel_volatility_state"] = volatilityState
	ctx.signals["channel_position_state"] = positionState
	ctx.signals["channel_fake_risk"] = fakeRisk
}

func addTradingViewSemanticAnalysis(ctx *analysisContext) {
	qqeTrend, _ := latestSignal(ctx, "qqe_trend")
	utDirection, _ := latestSignal(ctx, "ut_direction")
	sslDirection, _ := latestSignal(ctx, "ssl_direction")
	rangeDirection, _ := latestSignal(ctx, "range_filter_direction")
	nwTrend, _ := latestSignal(ctx, "nw_trend")

	qqeBias := directionBias(qqeTrend)
	utBias := directionBias(utDirection)
	sslBias := directionBias(sslDirection)
	rangeBias := directionBias(rangeDirection)
	nwBias := directionBias(nwTrend)

	ctx.signals["qqe_window_bias"] = qqeBias
	ctx.signals["ut_window_direction"] = utBias
	ctx.signals["ssl_window_bias"] = sslBias
	ctx.signals["range_filter_window_state"] = rangeDirectionState(rangeDirection)
	ctx.signals["nw_window_bias"] = nwBias

	score := 0
	for _, bias := range []string{qqeBias, utBias, sslBias, rangeBias, nwBias} {
		switch bias {
		case "bull":
			score++
		case "bear":
			score--
		}
	}
	trendBias := "neutral"
	switch {
	case score >= 2:
		trendBias = "bull"
	case score <= -2:
		trendBias = "bear"
	}
	ctx.signals["tradingview_window_bias"] = trendBias
	setInt(ctx.values, "tradingview_window_score", score)

	exhaustionRisk := "low"
	if value, ok := latestSignal(ctx, "td_exhaustion"); ok && !signalIs(value, "none", "neutral") {
		exhaustionRisk = "medium"
	}
	if value, ok := latestSignal(ctx, "wvf_state"); ok && signalIs(value, "panic") {
		exhaustionRisk = "high"
	}
	if value, ok := latestSignal(ctx, "nw_position_state"); ok && !signalIs(value, "inside") {
		if exhaustionRisk == "low" {
			exhaustionRisk = "medium"
		}
	}
	ctx.signals["exhaustion_risk"] = exhaustionRisk
}

func breakoutBias(value string) string {
	switch {
	case signalIs(value, "breakout_up"):
		return "bull"
	case signalIs(value, "breakout_down"):
		return "bear"
	default:
		return "neutral"
	}
}

func oppositeBias(value string) string {
	switch value {
	case "bull":
		return "bear"
	case "bear":
		return "bull"
	default:
		return "neutral"
	}
}

func channelWidthState(direction string) string {
	switch {
	case signalIs(direction, "rising"):
		return "expanding"
	case signalIs(direction, "falling"):
		return "contracting"
	default:
		return "flat"
	}
}

func channelPositionState(position float64) string {
	switch {
	case position >= 0.8:
		return "upper"
	case position <= 0.2:
		return "lower"
	default:
		return "middle"
	}
}

func rangeDirectionState(direction string) string {
	switch {
	case signalIs(direction, "up"):
		return "up"
	case signalIs(direction, "down"):
		return "down"
	default:
		return "flat"
	}
}

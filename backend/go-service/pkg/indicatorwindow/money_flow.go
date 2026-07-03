package indicatorwindow

func addMoneyFlowWindowAnalysis(ctx *analysisContext) {
	ctx.addNumeric(
		"volume_ma20", "volume_ratio20", "volume_ratio5", "volume_ratio10",
		"volume_breakout_ratio", "volume_trend5", "volume_zscore20",
		"volume_divergence_score", "volume_pressure20",
		"obv", "obv_slope5",
		"vwap", "vwap_distance_pct", "rolling_vwap20", "rolling_vwap_distance_pct",
		"mfi14", "cmf20", "ad_line", "ad_line_slope5",
		"price_volume_trend",
		"dynamic_swing_vwap", "dynamic_swing_vwap_distance_pct",
		"dynamic_swing_vwap_anchor_price", "dynamic_swing_vwap_anchor_age",
		"volume_profile_poc", "volume_profile_vah", "volume_profile_val",
		"volume_profile_range_high", "volume_profile_range_low",
		"volume_profile_value_area_pct",
		"volume_profile_poc_distance_pct",
		"volume_profile_vah_distance_pct", "volume_profile_val_distance_pct",
	)
	ctx.addSignals(
		"money_flow", "volume_state", "price_volume_confirmation", "cmf_state",
		"price_volume_action", "breakout_volume_confirm", "breakout_volume_strength",
		"volume_divergence", "volume_phase",
		"dynamic_swing_vwap_direction", "dynamic_swing_vwap_position",
		"dynamic_swing_vwap_anchor_type", "dynamic_swing_vwap_swing_label",
		"volume_profile_position", "volume_profile_poc_side", "volume_profile_value_area_state",
	)
	addMoneyFlowSemanticAnalysis(ctx)
	addVolumeProfileSemanticAnalysis(ctx)
}

func addMoneyFlowSemanticAnalysis(ctx *analysisContext) {
	volumeState := "normal"
	volumeSupport := false
	volumeClimax := false

	if stats, ok := numericStatsFor(ctx, "volume_ratio20"); ok {
		switch {
		case stats.latest >= volumeClimaxRatio:
			volumeState = "climax"
			volumeClimax = true
		case stats.latest >= volumeExpansionRatio:
			volumeState = "expansion"
		case stats.latest <= volumeDryRatio:
			volumeState = "dry"
		}
		ctx.values["volume_window_ratio"] = format(stats.latest)
	}
	if zscore, ok := latestNumeric(ctx, "volume_zscore20"); ok && zscore >= volumeClimaxZScore {
		volumeState = "climax"
		volumeClimax = true
	}
	if value, ok := latestSignal(ctx, "breakout_volume_confirm"); ok {
		volumeSupport = directionBias(value) != "neutral" || signalIs(value, "true", "yes", "confirmed")
	}
	if value, ok := latestSignal(ctx, "price_volume_confirmation"); ok {
		volumeSupport = volumeSupport || signalIs(value, "confirmed", "true", "yes", "bull", "bear")
	}

	moneyFlowBias := "neutral"
	if value, ok := latestSignal(ctx, "money_flow"); ok {
		moneyFlowBias = directionBias(value)
	}
	if moneyFlowBias == "neutral" {
		if cmf, ok := latestNumeric(ctx, "cmf20"); ok {
			switch {
			case cmf > cmfBullThreshold:
				moneyFlowBias = "bull"
			case cmf < cmfBearThreshold:
				moneyFlowBias = "bear"
			}
		}
	}

	ctx.signals["volume_window_state"] = volumeState
	ctx.signals["volume_window_support"] = boolSignal(volumeSupport)
	ctx.signals["volume_window_climax"] = boolSignal(volumeClimax)
	ctx.signals["money_flow_window_bias"] = moneyFlowBias
	ctx.signals["price_volume_window_confirmed"] = boolSignal(volumeSupport)
}

func addVolumeProfileSemanticAnalysis(ctx *analysisContext) {
	position := "unknown"
	if value, ok := latestSignal(ctx, "volume_profile_position"); ok {
		position = normalizeSignal(value)
	}
	pocSide := "unknown"
	if value, ok := latestSignal(ctx, "volume_profile_poc_side"); ok {
		pocSide = normalizeSignal(value)
	}
	valueAreaState := "unknown"
	if value, ok := latestSignal(ctx, "volume_profile_value_area_state"); ok {
		valueAreaState = normalizeSignal(value)
	}

	bias := "neutral"
	switch valueAreaState {
	case "upper_breakout":
		bias = "bull"
	case "lower_breakdown":
		bias = "bear"
	}

	acceptance := "balanced"
	switch {
	case signalIs(position, "inside_value_area"):
		acceptance = "inside_value_area"
	case signalIs(position, "above_value_area", "below_value_area"):
		if signalStableCountFor(ctx, "volume_profile_position") >= 2 {
			acceptance = "accepted_outside_value_area"
		} else {
			acceptance = "testing_value_area_edge"
		}
	}

	rejectionRisk := false
	nearPOC := false
	if pocDistance, ok := latestNumeric(ctx, "volume_profile_poc_distance_pct"); ok &&
		pocDistance > -volumeProfileNearPOCPct && pocDistance < volumeProfileNearPOCPct {
		nearPOC = true
	}
	nearValueEdge := false
	if vahDistance, ok := latestNumeric(ctx, "volume_profile_vah_distance_pct"); ok &&
		vahDistance > -volumeProfileNearValueEdgePct && vahDistance < volumeProfileNearValueEdgePct {
		nearValueEdge = true
	}
	if valDistance, ok := latestNumeric(ctx, "volume_profile_val_distance_pct"); ok &&
		valDistance > -volumeProfileNearValueEdgePct && valDistance < volumeProfileNearValueEdgePct {
		nearValueEdge = true
	}
	if signalIs(position, "above_value_area", "below_value_area") &&
		signalStableCountFor(ctx, "volume_profile_position") <= 1 {
		rejectionRisk = true
	}
	if nearPOC {
		rejectionRisk = true
	}

	breakoutQuality := "neutral"
	if bias != "neutral" {
		breakoutQuality = "weak"
		if signalIs(ctx.signals["volume_window_support"], "true") &&
			acceptance == "accepted_outside_value_area" {
			breakoutQuality = "confirmed"
		}
	}

	ctx.signals["volume_profile_window_position"] = position
	ctx.signals["volume_profile_window_poc_side"] = pocSide
	ctx.signals["volume_profile_window_bias"] = bias
	ctx.signals["volume_profile_window_acceptance"] = acceptance
	ctx.signals["volume_profile_window_near_poc"] = boolSignal(nearPOC)
	ctx.signals["volume_profile_window_near_value_edge"] = boolSignal(nearValueEdge)
	ctx.signals["volume_profile_window_rejection_risk"] = boolSignal(rejectionRisk)
	ctx.signals["volume_profile_window_breakout_quality"] = breakoutQuality
}

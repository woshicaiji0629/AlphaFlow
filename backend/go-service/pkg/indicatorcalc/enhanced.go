package indicatorcalc

func addEnhancedToSet(target *ValueSet, values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64, volumes []float64, basic *basicIndicatorState, features *featureContext, preview *aiSourceResult) {
	addTrendFeaturesWithContextToSet(target, values, signals, closes, features)
	addCandlePatterns(signals, opens, highs, lows, closes)
	addHeikinAshiFeaturesToSet(target, values, signals, opens, highs, lows, closes, basic)
	addSupportResistanceWithFeaturesToSet(target, values, signals, highs, lows, closes, features)
	addFibonacciFeaturesToSet(target, values, signals, highs, lows, closes)
	addPivotPointFeaturesToSet(target, values, signals, highs, lows, closes)
	addSupertrendWithStateAndFeaturesToSet(target, values, signals, highs, lows, closes, 10, 3, basic, features)
	addAlphaTrendToSet(target, values, signals, highs, lows, closes, volumes, 14, 1, basic)
	addPSARFeaturesWithStateToSet(target, values, signals, highs, lows, closes, basic)
	addChandelierExitToSet(target, values, signals, highs, lows, closes, 22, 3, basic)
	addIchimokuFeaturesToSet(target, values, signals, highs, lows, closes)
	addMoneyFlowFeaturesToSet(target, values, signals, highs, lows, closes, volumes, basic)
	addDynamicSwingAnchoredVWAPToSet(target, values, signals, highs, lows, closes, volumes, basic)
	addSqueezeMomentumToSet(target, values, signals, highs, lows, closes)
	addBollingerFeaturesWithContextToSet(target, values, signals, closes, features)
	addChannelFeaturesWithContextToSet(target, values, signals, highs, lows, closes, features)
	addTradingViewFeaturesWithContextToSet(target, values, signals, highs, lows, closes, features)
	addSmartMoneyToSet(target, values, signals, opens, highs, lows, closes)
	addLivermoreFeaturesToSet(target, values, signals, highs, lows, closes, opens)
	if preview == nil {
		addAISourceSwitchingFeaturesWithContextToSet(target, values, signals, opens, highs, lows, closes, features)
	} else {
		setValueTarget(target, values, "ai_source_ma", preview.ma, true)
		setValueTarget(target, values, "ai_source_value", preview.sourceValue, true)
		setValueTarget(target, values, "ai_source_drive", preview.drive, true)
		setValueTarget(target, values, "ai_source_score_open", preview.scoreOpen, true)
		setValueTarget(target, values, "ai_source_score_high", preview.scoreHigh, true)
		setValueTarget(target, values, "ai_source_score_low", preview.scoreLow, true)
		setValueTarget(target, values, "ai_source_score_close", preview.scoreClose, true)
		setValueTarget(target, values, "ai_source_supertrend", preview.supertrend, true)
		setValueTarget(target, values, "ai_source_supertrend_distance_pct", preview.supertrendDist, true)
		setValueTarget(target, values, "ai_source_supertrend_adapt_mult", preview.adaptMultiplier, true)
		signals["ai_source_selected"] = preview.selected
		signals["ai_source_changed"] = aiSourceBoolSignal(preview.changed)
		signals["ai_source_supertrend_direction"] = preview.direction
		signals["ai_source_supertrend_flip"] = preview.flip
		signals["ai_source_ready"] = aiSourceBoolSignal(preview.ready)
	}
}

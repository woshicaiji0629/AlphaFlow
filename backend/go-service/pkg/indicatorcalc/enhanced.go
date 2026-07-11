package indicatorcalc

func addEnhanced(values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64, volumes []float64, basic *basicIndicatorState, features *featureContext, preview *aiSourceResult) {
	addTrendFeaturesWithContext(values, signals, closes, features)
	addCandlePatterns(signals, opens, highs, lows, closes)
	addHeikinAshiFeatures(values, signals, opens, highs, lows, closes)
	addSupportResistance(values, signals, highs, lows, closes)
	addFibonacciFeatures(values, signals, highs, lows, closes)
	addPivotPointFeatures(values, signals, highs, lows, closes)
	addSupertrend(values, signals, highs, lows, closes, 10, 3)
	addAlphaTrend(values, signals, highs, lows, closes, volumes, 14, 1)
	addPSARFeatures(values, signals, highs, lows, closes)
	addChandelierExit(values, signals, highs, lows, closes, 22, 3)
	addIchimokuFeatures(values, signals, highs, lows, closes)
	addMoneyFlowFeatures(values, signals, highs, lows, closes, volumes, basic)
	addDynamicSwingAnchoredVWAP(values, signals, highs, lows, closes, volumes)
	addSqueezeMomentum(values, signals, highs, lows, closes)
	addBollingerFeaturesWithContext(values, signals, closes, features)
	addChannelFeaturesWithContext(values, signals, highs, lows, closes, features)
	addTradingViewFeaturesWithContext(values, signals, highs, lows, closes, features)
	addSmartMoney(values, signals, opens, highs, lows, closes)
	addLivermoreFeatures(values, signals, highs, lows, closes, opens)
	if preview == nil {
		addAISourceSwitchingFeaturesWithContext(values, signals, opens, highs, lows, closes, features)
	} else {
		setValue(values, "ai_source_ma", preview.ma, true)
		setValue(values, "ai_source_value", preview.sourceValue, true)
		setValue(values, "ai_source_drive", preview.drive, true)
		setValue(values, "ai_source_score_open", preview.scoreOpen, true)
		setValue(values, "ai_source_score_high", preview.scoreHigh, true)
		setValue(values, "ai_source_score_low", preview.scoreLow, true)
		setValue(values, "ai_source_score_close", preview.scoreClose, true)
		setValue(values, "ai_source_supertrend", preview.supertrend, true)
		setValue(values, "ai_source_supertrend_distance_pct", preview.supertrendDist, true)
		setValue(values, "ai_source_supertrend_adapt_mult", preview.adaptMultiplier, true)
		signals["ai_source_selected"] = preview.selected
		signals["ai_source_changed"] = aiSourceBoolSignal(preview.changed)
		signals["ai_source_supertrend_direction"] = preview.direction
		signals["ai_source_supertrend_flip"] = preview.flip
		signals["ai_source_ready"] = aiSourceBoolSignal(preview.ready)
	}
}

package indicatorcalc

func addTradingViewFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	addTradingViewFeaturesWithContextToSet(nil, values, signals, highs, lows, closes, nil)
}

func addTradingViewFeaturesWithContextToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, features *featureContext) {
	var basic *basicIndicatorState
	if features != nil {
		basic = features.basic
	}
	if !addStreamQQEFeatures(target, values, signals, basic.qqe6State()) {
		rsi6, rsi6OK := rsiSeries(closes, 6)
		smoothed, smoothedDeltas, offset, foundationOK := qqeModTrendFoundation(rsi6, rsi6OK, 6, 5)
		addQQEModFeaturesWithFoundation(target, values, signals, smoothed, smoothedDeltas, 6, 5, 3, foundationOK)
		addQQEModEnhancedFeaturesWithFoundation(target, values, signals, smoothed, smoothedDeltas, offset, foundationOK)
	}
	if features != nil {
		if atrValues, ok := features.atrSeries(10); ok {
			addUTBotFeaturesWithATR(target, values, signals, closes, 10, 1, atrValues, features.basic)
		} else {
			addUTBotFeatures(target, values, signals, highs, lows, closes, 10, 1)
		}
	} else {
		addUTBotFeatures(target, values, signals, highs, lows, closes, 10, 1)
	}
	addSSLChannelFeatures(target, values, signals, highs, lows, closes, 10, basic)
	addRangeFilterFeatures(target, values, signals, closes, 100, 3, basic)
	addWilliamsVixFixFeatures(target, values, signals, lows, closes, 22, 20, 2, 50, 0.85, basic)
	addTDSequentialFeatures(target, values, signals, closes, basic)
	addNadarayaWatsonEnvelopeFeatures(target, values, signals, closes, 50, 8, 3, basic)
}

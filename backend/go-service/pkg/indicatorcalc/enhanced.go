package indicatorcalc

func addEnhanced(values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64, volumes []float64) {
	addTrendFeatures(values, signals, closes)
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
	addMoneyFlowFeatures(values, signals, highs, lows, closes, volumes)
	addDynamicSwingAnchoredVWAP(values, signals, highs, lows, closes, volumes)
	addSqueezeMomentum(values, signals, highs, lows, closes)
	addBollingerFeatures(values, signals, closes)
	addChannelFeatures(values, signals, highs, lows, closes)
	addTradingViewFeatures(values, signals, highs, lows, closes)
	addSmartMoney(values, signals, opens, highs, lows, closes)
	addLivermoreFeatures(values, signals, highs, lows, closes, opens)
}

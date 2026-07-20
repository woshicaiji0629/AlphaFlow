package indicatorcalc

func addRSIFeatures(values map[string]string, signals map[string]string, closes []float64, period int) {
	series, ok := rsiSeries(closes, period)
	if !ok {
		return
	}
	addRSIFeaturesFromSeries(values, signals, closes, series)
}

func addRSIFeaturesFromSeries(values map[string]string, signals map[string]string, closes []float64, series []float64) {
	addRSIFeaturesFromSeriesToSet(nil, values, signals, closes, series)
}

func addRSIFeaturesFromSeriesToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, series []float64) {
	if len(series) == 0 {
		return
	}
	value := series[len(series)-1]
	setValueTarget(target, values, "rsi14", value, true)
	if previousIndex := len(series) - 4; previousIndex >= 0 {
		setValueTarget(target, values, "rsi_slope3", value-series[previousIndex], true)
	}
	signals["rsi_state"] = rsiState(value)
	signals["rsi_divergence"] = rsiDivergenceFromSeries(closes, series)
}

func addOscillatorFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	rsi14Series, _ := rsiSeries(closes, 14)
	addOscillatorFeaturesWithRSI(values, signals, highs, lows, closes, rsi14Series, nil)
}

func addOscillatorFeaturesWithRSI(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, rsi14Series []float64, basic *basicIndicatorState) {
	addOscillatorFeaturesWithRSIToSet(nil, values, signals, highs, lows, closes, rsi14Series, basic)
}

func addOscillatorFeaturesWithRSIToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, rsi14Series []float64, basic *basicIndicatorState) {
	k, d, j, ok := kdj(highs, lows, closes, 9)
	if ok {
		setValueTarget(target, values, "kdj_k", k, true)
		setValueTarget(target, values, "kdj_d", d, true)
		setValueTarget(target, values, "kdj_j", j, true)
	}
	stochK, stochD, ok := stochastic(highs, lows, closes, 14, 3)
	if ok {
		setValueTarget(target, values, "stoch_k", stochK, true)
		setValueTarget(target, values, "stoch_d", stochD, true)
	}
	stochRSIK, stochRSID, ok := stochRSIFromSeries(rsi14Series, 14, 3)
	if ok {
		setValueTarget(target, values, "stoch_rsi_k", stochRSIK, true)
		setValueTarget(target, values, "stoch_rsi_d", stochRSID, true)
		signals["stoch_rsi_state"] = oscillatorState(stochRSIK, 80, 20)
	}
	skdjK, skdjD, previousK, previousD, ok := skdj(highs, lows, closes, 9, 3)
	if ok {
		setValueTarget(target, values, "skdj_k", skdjK, true)
		setValueTarget(target, values, "skdj_d", skdjD, true)
		signals["skdj_cross"] = crossSignal(previousK, previousD, skdjK, skdjD)
	}
	cciValue, ok := cci(highs, lows, closes, 20)
	if ok {
		setValueTarget(target, values, "cci20", cciValue, true)
		signals["cci_state"] = oscillatorState(cciValue, 100, -100)
	}
	williamsValue, ok := williamsR(highs, lows, closes, 14)
	if ok {
		setValueTarget(target, values, "williams_r14", williamsValue, true)
		signals["williams_state"] = williamsState(williamsValue)
	}
	rocValue, ok := roc(closes, 12)
	if ok {
		setValueTarget(target, values, "roc12", rocValue, true)
		signals["roc_state"] = rocState(rocValue)
	}
	addWaveTrendFeaturesToSet(target, values, signals, highs, lows, closes, basic)
}

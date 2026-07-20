package indicatorcalc

func addMoneyFlowFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64, basic *basicIndicatorState) {
	addMoneyFlowFeaturesToSet(nil, values, signals, highs, lows, closes, volumes, basic)
}

func addMoneyFlowFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64, basic *basicIndicatorState) {
	if len(closes) < 20 || len(volumes) != len(closes) {
		return
	}
	last := len(closes) - 1
	mfi := moneyFlowIndex(highs, lows, closes, volumes, 14)
	setValueTarget(target, values, "mfi14", mfi, true)

	vwapValue, streamVWAPOK := basic.vwapValue(closes[last])
	if !streamVWAPOK {
		vwapValue = vwap(highs, lows, closes, volumes)
	}
	setValueTarget(target, values, "vwap_distance_pct", percentDistance(closes[last], vwapValue), vwapValue != 0)
	rollingVWAP, ok := rollingVWAP(highs, lows, closes, volumes, 20)
	setValueTarget(target, values, "rolling_vwap20", rollingVWAP, ok)
	setValueTarget(target, values, "rolling_vwap_distance_pct", percentDistance(closes[last], rollingVWAP), ok && rollingVWAP != 0)

	obvSlope, pvt, pvtSlope, adLine, adLineSlope, streamMoneyFlowOK := moneyFlowStateValues(basic, closes)
	if !streamMoneyFlowOK {
		obvSeries := obvSeries(closes, volumes)
		obvSlope = slope(obvSeries, 5)
		pvtSeries := priceVolumeTrendSeries(closes, volumes)
		pvt = pvtSeries[len(pvtSeries)-1]
		pvtSlope = slope(pvtSeries, 5)
		adValues := accumulationDistributionSeries(highs, lows, closes, volumes)
		adLine = adValues[len(adValues)-1]
		adLineSlope = slope(adValues, 5)
		signals["price_volume_confirmation"] = priceVolumeConfirmation(closes, obvSeries, pvtSeries)
	} else {
		signals["price_volume_confirmation"] = priceVolumeConfirmationFromSlopes(closes, obvSlope, pvtSlope)
	}
	setValueTarget(target, values, "obv_slope5", obvSlope, len(closes) >= 5)

	volumeZScore, ok := zScore(volumes, 20)
	setValueTarget(target, values, "volume_zscore20", volumeZScore, ok)
	volumeRatio5, ok5 := volumeRatio(volumes, 5)
	setValueTarget(target, values, "volume_ratio5", volumeRatio5, ok5)
	volumeRatio10, ok10 := volumeRatio(volumes, 10)
	setValueTarget(target, values, "volume_ratio10", volumeRatio10, ok10)
	volumeBreakoutRatio, okBreakout := volumeBreakoutRatio(volumes, 20)
	setValueTarget(target, values, "volume_breakout_ratio", volumeBreakoutRatio, okBreakout)
	setValueTarget(target, values, "volume_trend5", slope(volumes, 5), len(volumes) >= 5)
	divergenceScore := volumeDivergenceScore(closes, volumes, 20)
	setValueTarget(target, values, "volume_divergence_score", divergenceScore, len(closes) >= 20)
	pressure := volumePressure(closes, volumes, 20)
	setValueTarget(target, values, "volume_pressure20", pressure, true)

	setValueTarget(target, values, "price_volume_trend", pvt, true)
	cmfValue, ok := chaikinMoneyFlow(highs, lows, closes, volumes, 20)
	setValueTarget(target, values, "cmf20", cmfValue, ok)
	setValueTarget(target, values, "ad_line", adLine, true)
	setValueTarget(target, values, "ad_line_slope5", adLineSlope, len(closes) >= 5)

	signals["money_flow"] = moneyFlowSignal(mfi, pressure)
	signals["volume_state"] = volumeState(volumeZScore, ok)
	signals["cmf_state"] = cmfState(cmfValue, ok)
	signals["price_volume_action"] = priceVolumeAction(closes, volumes, volumeRatio5, ok5)
	signals["breakout_volume_confirm"] = breakoutVolumeConfirm(highs, closes, volumeBreakoutRatio, okBreakout)
	signals["breakout_volume_strength"] = breakoutVolumeStrength(volumeBreakoutRatio, okBreakout)
	signals["volume_divergence"] = volumeDivergenceFromScore(divergenceScore)
	signals["volume_phase"] = volumePhase(pressure, cmfValue, ok)
	addVolumeFlowIndicatorFeaturesToSet(target, values, signals, highs, lows, closes, volumes, 130, 0.2, 2.5, 5, basic)
	addVolumeProfileFeaturesToSet(target, values, signals, highs, lows, closes, volumes, 200, 100, 68)
	addSupplyDemandRangeFeaturesToSet(target, values, signals, highs, lows, closes, volumes, 120, 50, 10)
}

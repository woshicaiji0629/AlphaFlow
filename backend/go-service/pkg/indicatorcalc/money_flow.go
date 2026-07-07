package indicatorcalc

import "math"

func addMoneyFlowFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64, basic *basicIndicatorState) {
	if len(closes) < 20 || len(volumes) != len(closes) {
		return
	}
	last := len(closes) - 1
	mfi := moneyFlowIndex(highs, lows, closes, volumes, 14)
	setValue(values, "mfi14", mfi, true)

	vwapValue := vwap(highs, lows, closes, volumes)
	setValue(values, "vwap_distance_pct", percentDistance(closes[last], vwapValue), vwapValue != 0)
	rollingVWAP, ok := rollingVWAP(highs, lows, closes, volumes, 20)
	setValue(values, "rolling_vwap20", rollingVWAP, ok)
	setValue(values, "rolling_vwap_distance_pct", percentDistance(closes[last], rollingVWAP), ok && rollingVWAP != 0)

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
	setValue(values, "obv_slope5", obvSlope, len(closes) >= 5)

	volumeZScore, ok := zScore(volumes, 20)
	setValue(values, "volume_zscore20", volumeZScore, ok)
	volumeRatio5, ok5 := volumeRatio(volumes, 5)
	setValue(values, "volume_ratio5", volumeRatio5, ok5)
	volumeRatio10, ok10 := volumeRatio(volumes, 10)
	setValue(values, "volume_ratio10", volumeRatio10, ok10)
	volumeBreakoutRatio, okBreakout := volumeBreakoutRatio(volumes, 20)
	setValue(values, "volume_breakout_ratio", volumeBreakoutRatio, okBreakout)
	setValue(values, "volume_trend5", slope(volumes, 5), len(volumes) >= 5)
	setValue(values, "volume_divergence_score", volumeDivergenceScore(closes, volumes, 20), len(closes) >= 20)
	pressure := volumePressure(closes, volumes, 20)
	setValue(values, "volume_pressure20", pressure, true)

	setValue(values, "price_volume_trend", pvt, true)
	cmfValue, ok := chaikinMoneyFlow(highs, lows, closes, volumes, 20)
	setValue(values, "cmf20", cmfValue, ok)
	setValue(values, "ad_line", adLine, true)
	setValue(values, "ad_line_slope5", adLineSlope, len(closes) >= 5)

	signals["money_flow"] = moneyFlowSignal(mfi, pressure)
	signals["volume_state"] = volumeState(volumeZScore, ok)
	signals["cmf_state"] = cmfState(cmfValue, ok)
	signals["price_volume_action"] = priceVolumeAction(closes, volumes, volumeRatio5, ok5)
	signals["breakout_volume_confirm"] = breakoutVolumeConfirm(highs, closes, volumeBreakoutRatio, okBreakout)
	signals["breakout_volume_strength"] = breakoutVolumeStrength(volumeBreakoutRatio, okBreakout)
	signals["volume_divergence"] = volumeDivergence(closes, volumes, 20)
	signals["volume_phase"] = volumePhase(pressure, cmfValue, ok)
	addVolumeFlowIndicatorFeatures(values, signals, highs, lows, closes, volumes, 130, 0.2, 2.5, 5)
	addVolumeProfileFeatures(values, signals, highs, lows, closes, volumes, 200, 100, 68)
	addSupplyDemandRangeFeatures(values, signals, highs, lows, closes, volumes, 120, 50, 10)
}

type volumeFlowIndicatorResult struct {
	value          float64
	signal         float64
	hist           float64
	previousValue  float64
	previousSignal float64
	volumeCutoff   float64
	priceCutoff    float64
}

func addVolumeFlowIndicatorFeatures(
	values map[string]string,
	signals map[string]string,
	highs []float64,
	lows []float64,
	closes []float64,
	volumes []float64,
	length int,
	coef float64,
	volumeCoef float64,
	signalLength int,
) {
	result, ok := volumeFlowIndicatorCompact(highs, lows, closes, volumes, length, coef, volumeCoef, signalLength)
	if !ok {
		result, ok = volumeFlowIndicator(highs, lows, closes, volumes, length, coef, volumeCoef, signalLength)
	}
	if !ok {
		return
	}
	setValue(values, "vfi", result.value, true)
	setValue(values, "vfi_signal", result.signal, true)
	setValue(values, "vfi_hist", result.hist, true)
	setValue(values, "vfi_volume_cutoff", result.volumeCutoff, true)
	setValue(values, "vfi_price_cutoff", result.priceCutoff, true)
	signals["vfi_state"] = vfiState(result.value)
	signals["vfi_cross"] = crossSignal(result.previousValue, result.previousSignal, result.value, result.signal)
	signals["vfi_momentum"] = vfiMomentum(result.hist)
}

func volumeFlowIndicatorCompact(
	highs []float64,
	lows []float64,
	closes []float64,
	volumes []float64,
	length int,
	coef float64,
	volumeCoef float64,
	signalLength int,
) (volumeFlowIndicatorResult, bool) {
	if length <= 0 || coef <= 0 || volumeCoef <= 0 || signalLength <= 0 ||
		len(closes) != len(highs) || len(closes) != len(lows) || len(closes) != len(volumes) ||
		len(closes) < length*2+signalLength {
		return volumeFlowIndicatorResult{}, false
	}
	signalEMA := newStreamEMAState(signalLength)
	var vcpSum float64
	invalidCount := 1
	interWindow := make([]float64, 30)
	vcpWindow := make([]float64, length)
	validWindow := make([]bool, length)
	previousTypical := (highs[0] + lows[0] + closes[0]) / 3
	var volumeSum float64
	vfiCount := 0
	previousValue := 0.0
	currentValue := 0.0
	lastPriceCutoff := 0.0
	lastVolumeCutoff := 0.0
	for index := 1; index < len(closes); index++ {
		volumeSum += volumes[index-1]
		if dropVolume := index - length - 1; dropVolume >= 0 {
			volumeSum -= volumes[dropVolume]
		}

		typical := (highs[index] + lows[index] + closes[index]) / 3
		interValue := 0.0
		if typical > 0 && previousTypical > 0 {
			interValue = math.Log(typical) - math.Log(previousTypical)
		}
		interWindow[index%len(interWindow)] = interValue

		vcpValue := 0.0
		validVCP := false
		priceCutoff := 0.0
		volumeCutoff := 0.0
		if index >= length && index >= len(interWindow) && volumeSum != 0 {
			volatility := standardDeviationRing(interWindow, index, len(interWindow))
			volumeAverage := volumeSum / float64(length)
			priceCutoff = coef * volatility * closes[index]
			volumeCutoff = volumeAverage * volumeCoef
			cappedVolume := volumes[index]
			if cappedVolume > volumeCutoff {
				cappedVolume = volumeCutoff
			}
			moneyFlow := typical - previousTypical
			switch {
			case moneyFlow > priceCutoff:
				vcpValue = cappedVolume
			case moneyFlow < -priceCutoff:
				vcpValue = -cappedVolume
			default:
				vcpValue = 0
			}
			validVCP = true
		}

		slot := index % length
		if index >= length {
			vcpSum -= vcpWindow[slot]
			if !validWindow[slot] {
				invalidCount--
			}
		}
		vcpWindow[slot] = vcpValue
		validWindow[slot] = validVCP
		vcpSum += vcpValue
		if !validVCP {
			invalidCount++
		}
		previousTypical = typical

		if index < length-1 || invalidCount > 0 || volumeSum == 0 {
			continue
		}
		volumeAverage := volumeSum / float64(length)
		value := vcpSum / volumeAverage
		if vfiCount > 0 {
			previousValue = currentValue
		}
		currentValue = value
		vfiCount++
		signalEMA.append(value)
		if !signalEMA.ready {
			continue
		}
		lastPriceCutoff = priceCutoff
		lastVolumeCutoff = volumeCutoff
	}
	if vfiCount < signalLength+1 || !signalEMA.ready || !signalEMA.hasPrevious {
		return volumeFlowIndicatorResult{}, false
	}
	return volumeFlowIndicatorResult{
		value:          currentValue,
		signal:         signalEMA.value,
		hist:           currentValue - signalEMA.value,
		previousValue:  previousValue,
		previousSignal: signalEMA.previous,
		volumeCutoff:   lastVolumeCutoff,
		priceCutoff:    lastPriceCutoff,
	}, true
}

func volumeFlowIndicator(
	highs []float64,
	lows []float64,
	closes []float64,
	volumes []float64,
	length int,
	coef float64,
	volumeCoef float64,
	signalLength int,
) (volumeFlowIndicatorResult, bool) {
	if length <= 0 || coef <= 0 || volumeCoef <= 0 || signalLength <= 0 ||
		len(closes) != len(highs) || len(closes) != len(lows) || len(closes) != len(volumes) ||
		len(closes) < length*2+signalLength {
		return volumeFlowIndicatorResult{}, false
	}
	typicals := make([]float64, len(closes))
	inter := make([]float64, len(closes))
	for index := range closes {
		typicals[index] = (highs[index] + lows[index] + closes[index]) / 3
		if index == 0 || typicals[index] <= 0 || typicals[index-1] <= 0 {
			continue
		}
		inter[index] = math.Log(typicals[index]) - math.Log(typicals[index-1])
	}
	vcp := make([]float64, len(closes))
	validVCP := make([]bool, len(closes))
	priceCutoffs := make([]float64, len(closes))
	volumeCutoffs := make([]float64, len(closes))
	for index := 1; index < len(closes); index++ {
		if index < length || index < 30 {
			continue
		}
		volatility, ok := standardDeviation(inter[:index+1], 30)
		if !ok {
			continue
		}
		volumeAverage, ok := sma(volumes[index-length:index], length)
		if !ok || volumeAverage == 0 {
			continue
		}
		priceCutoff := coef * volatility * closes[index]
		volumeCutoff := volumeAverage * volumeCoef
		cappedVolume := volumes[index]
		if cappedVolume > volumeCutoff {
			cappedVolume = volumeCutoff
		}
		moneyFlow := typicals[index] - typicals[index-1]
		switch {
		case moneyFlow > priceCutoff:
			vcp[index] = cappedVolume
		case moneyFlow < -priceCutoff:
			vcp[index] = -cappedVolume
		default:
			vcp[index] = 0
		}
		validVCP[index] = true
		priceCutoffs[index] = priceCutoff
		volumeCutoffs[index] = volumeCutoff
	}
	vfiValues := []float64{}
	priceCutoffValues := []float64{}
	volumeCutoffValues := []float64{}
	for index := 0; index < len(closes); index++ {
		start := index - length + 1
		if start < 0 {
			continue
		}
		valid := true
		for vcpIndex := start; vcpIndex <= index; vcpIndex++ {
			if !validVCP[vcpIndex] {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		volumeAverage, ok := sma(volumes[index-length:index], length)
		if !ok || volumeAverage == 0 {
			continue
		}
		vfiValues = append(vfiValues, sum(vcp[start:index+1])/volumeAverage)
		priceCutoffValues = append(priceCutoffValues, priceCutoffs[index])
		volumeCutoffValues = append(volumeCutoffValues, volumeCutoffs[index])
	}
	if len(vfiValues) < signalLength+1 {
		return volumeFlowIndicatorResult{}, false
	}
	signalValues, ok := emaSeries(vfiValues, signalLength)
	if !ok || len(signalValues) < 2 {
		return volumeFlowIndicatorResult{}, false
	}
	offset := len(vfiValues) - len(signalValues)
	last := len(vfiValues) - 1
	prev := last - 1
	lastSignal := signalValues[len(signalValues)-1]
	prevSignal := signalValues[prev-offset]
	return volumeFlowIndicatorResult{
		value:          vfiValues[last],
		signal:         lastSignal,
		hist:           vfiValues[last] - lastSignal,
		previousValue:  vfiValues[prev],
		previousSignal: prevSignal,
		volumeCutoff:   volumeCutoffValues[len(volumeCutoffValues)-1],
		priceCutoff:    priceCutoffValues[len(priceCutoffValues)-1],
	}, true
}

func moneyFlowIndex(highs []float64, lows []float64, closes []float64, volumes []float64, period int) float64 {
	if len(closes) <= period {
		return 50
	}
	var positive float64
	var negative float64
	for index := len(closes) - period; index < len(closes); index++ {
		current := (highs[index] + lows[index] + closes[index]) / 3
		previous := (highs[index-1] + lows[index-1] + closes[index-1]) / 3
		flow := current * volumes[index]
		if current >= previous {
			positive += flow
		} else {
			negative += flow
		}
	}
	if negative == 0 {
		return 100
	}
	ratio := positive / negative
	return 100 - 100/(1+ratio)
}

func obvSeries(closes []float64, volumes []float64) []float64 {
	values := make([]float64, len(closes))
	for index := 1; index < len(closes); index++ {
		values[index] = values[index-1]
		switch {
		case closes[index] > closes[index-1]:
			values[index] += volumes[index]
		case closes[index] < closes[index-1]:
			values[index] -= volumes[index]
		}
	}
	return values
}

func priceVolumeTrendSeries(closes []float64, volumes []float64) []float64 {
	values := make([]float64, len(closes))
	for index := 1; index < len(closes); index++ {
		values[index] = values[index-1]
		if closes[index-1] != 0 {
			values[index] += (closes[index] - closes[index-1]) / closes[index-1] * volumes[index]
		}
	}
	return values
}

func moneyFlowStateValues(basic *basicIndicatorState, closes []float64) (float64, float64, float64, float64, float64, bool) {
	if len(closes) < 5 {
		return 0, 0, 0, 0, 0, false
	}
	_, obvSlope, pvt, pvtSlope, adLine, adLineSlope, ok := basic.moneyFlowValues()
	return obvSlope, pvt, pvtSlope, adLine, adLineSlope, ok
}

func rollingVWAP(highs []float64, lows []float64, closes []float64, volumes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) < period || len(volumes) != len(closes) {
		return 0, false
	}
	start := len(closes) - period
	var weighted float64
	var volumeSum float64
	for index := start; index < len(closes); index++ {
		typical := (highs[index] + lows[index] + closes[index]) / 3
		weighted += typical * volumes[index]
		volumeSum += volumes[index]
	}
	if volumeSum == 0 {
		return 0, false
	}
	return weighted / volumeSum, true
}

func accumulationDistributionSeries(highs []float64, lows []float64, closes []float64, volumes []float64) []float64 {
	values := make([]float64, len(closes))
	for index := range closes {
		flowVolume := moneyFlowVolume(highs[index], lows[index], closes[index], volumes[index])
		if index == 0 {
			values[index] = flowVolume
			continue
		}
		values[index] = values[index-1] + flowVolume
	}
	return values
}

func chaikinMoneyFlow(highs []float64, lows []float64, closes []float64, volumes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) < period || len(volumes) != len(closes) {
		return 0, false
	}
	start := len(closes) - period
	var flowSum float64
	var volumeSum float64
	for index := start; index < len(closes); index++ {
		flowSum += moneyFlowVolume(highs[index], lows[index], closes[index], volumes[index])
		volumeSum += volumes[index]
	}
	if volumeSum == 0 {
		return 0, false
	}
	return flowSum / volumeSum, true
}

func moneyFlowVolume(high float64, low float64, closeValue float64, volume float64) float64 {
	if high == low {
		return 0
	}
	multiplier := ((closeValue - low) - (high - closeValue)) / (high - low)
	return multiplier * volume
}

func volumePressure(closes []float64, volumes []float64, period int) float64 {
	if period <= 0 || len(closes) < period {
		return 0
	}
	start := len(closes) - period
	var upVolume float64
	var downVolume float64
	for index := start; index < len(closes); index++ {
		switch {
		case index > 0 && closes[index] > closes[index-1]:
			upVolume += volumes[index]
		case index > 0 && closes[index] < closes[index-1]:
			downVolume += volumes[index]
		}
	}
	total := upVolume + downVolume
	if total == 0 {
		return 0
	}
	return (upVolume - downVolume) / total
}

func volumeRatio(volumes []float64, period int) (float64, bool) {
	if period <= 0 || len(volumes) < period+1 {
		return 0, false
	}
	previous, ok := sma(volumes[:len(volumes)-1], period)
	if !ok || previous == 0 {
		return 0, false
	}
	return volumes[len(volumes)-1] / previous, true
}

func volumeBreakoutRatio(volumes []float64, period int) (float64, bool) {
	if period <= 0 || len(volumes) < period+1 {
		return 0, false
	}
	previousMax := volumes[len(volumes)-period-1]
	for _, volume := range volumes[len(volumes)-period-1 : len(volumes)-1] {
		if volume > previousMax {
			previousMax = volume
		}
	}
	if previousMax == 0 {
		return 0, false
	}
	return volumes[len(volumes)-1] / previousMax, true
}

func addVolumeProfileFeatures(
	values map[string]string,
	signals map[string]string,
	highs []float64,
	lows []float64,
	closes []float64,
	volumes []float64,
	lookback int,
	bins int,
	valueAreaPct float64,
) {
	profile, ok := volumeProfile(highs, lows, closes, volumes, lookback, bins, valueAreaPct)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValue(values, "volume_profile_poc", profile.poc, true)
	setValue(values, "volume_profile_vah", profile.vah, true)
	setValue(values, "volume_profile_val", profile.val, true)
	setValue(values, "volume_profile_range_high", profile.rangeHigh, true)
	setValue(values, "volume_profile_range_low", profile.rangeLow, true)
	setValue(values, "volume_profile_value_area_pct", valueAreaPct, true)
	setValue(values, "volume_profile_poc_distance_pct", percentDistance(last, profile.poc), profile.poc != 0)
	setValue(values, "volume_profile_vah_distance_pct", percentDistance(last, profile.vah), profile.vah != 0)
	setValue(values, "volume_profile_val_distance_pct", percentDistance(last, profile.val), profile.val != 0)
	signals["volume_profile_position"] = volumeProfilePosition(last, profile.vah, profile.val)
	signals["volume_profile_poc_side"] = volumeProfilePOCSide(last, profile.poc)
	signals["volume_profile_value_area_state"] = volumeProfileValueAreaState(last, profile.vah, profile.val)
}

func addSupplyDemandRangeFeatures(
	values map[string]string,
	signals map[string]string,
	highs []float64,
	lows []float64,
	closes []float64,
	volumes []float64,
	lookback int,
	bins int,
	thresholdPct float64,
) {
	zone, ok := supplyDemandRange(highs, lows, closes, volumes, lookback, bins, thresholdPct)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValue(values, "supply_zone_top", zone.supplyTop, true)
	setValue(values, "supply_zone_bottom", zone.supplyBottom, true)
	setValue(values, "supply_zone_avg", zone.supplyAvg, true)
	setValue(values, "supply_zone_wavg", zone.supplyWAvg, true)
	setValue(values, "demand_zone_top", zone.demandTop, true)
	setValue(values, "demand_zone_bottom", zone.demandBottom, true)
	setValue(values, "demand_zone_avg", zone.demandAvg, true)
	setValue(values, "demand_zone_wavg", zone.demandWAvg, true)
	setValue(values, "supply_demand_equilibrium", zone.equilibrium, true)
	setValue(values, "supply_demand_weighted_equilibrium", zone.weightedEquilibrium, true)
	signals["supply_demand_position"] = supplyDemandPosition(last, zone)
}

type volumeProfileResult struct {
	poc       float64
	vah       float64
	val       float64
	rangeHigh float64
	rangeLow  float64
}

type supplyDemandRangeResult struct {
	supplyTop           float64
	supplyBottom        float64
	supplyAvg           float64
	supplyWAvg          float64
	demandTop           float64
	demandBottom        float64
	demandAvg           float64
	demandWAvg          float64
	equilibrium         float64
	weightedEquilibrium float64
}

func volumeProfile(
	highs []float64,
	lows []float64,
	closes []float64,
	volumes []float64,
	lookback int,
	bins int,
	valueAreaPct float64,
) (volumeProfileResult, bool) {
	if lookback <= 0 || bins < 2 || len(closes) < lookback || len(highs) != len(closes) ||
		len(lows) != len(closes) || len(volumes) != len(closes) {
		return volumeProfileResult{}, false
	}
	start := len(closes) - lookback
	rangeHigh := highs[start]
	rangeLow := lows[start]
	for index := start + 1; index < len(closes); index++ {
		rangeHigh = math.Max(rangeHigh, highs[index])
		rangeLow = math.Min(rangeLow, lows[index])
	}
	if rangeHigh <= rangeLow {
		return volumeProfileResult{}, false
	}

	bucketSize := (rangeHigh - rangeLow) / float64(bins-1)
	if bucketSize <= 0 {
		return volumeProfileResult{}, false
	}
	bucketVolumes := make([]float64, bins)
	for index := start; index < len(closes); index++ {
		lowBucket := volumeProfileBucketIndex(lows[index], rangeLow, bucketSize, bins)
		highBucket := volumeProfileBucketIndex(highs[index], rangeLow, bucketSize, bins)
		if highBucket < lowBucket {
			continue
		}
		coveredBuckets := highBucket - lowBucket + 1
		if coveredBuckets <= 0 {
			continue
		}
		volumePerBucket := volumes[index] / float64(coveredBuckets)
		for bucket := lowBucket; bucket <= highBucket; bucket++ {
			bucketVolumes[bucket] += volumePerBucket
		}
	}

	maxIndex := 0
	totalVolume := 0.0
	for index, volume := range bucketVolumes {
		totalVolume += volume
		if volume > bucketVolumes[maxIndex] {
			maxIndex = index
		}
	}
	if totalVolume == 0 {
		return volumeProfileResult{}, false
	}

	valueAreaDown, valueAreaUp := volumeProfileValueArea(bucketVolumes, maxIndex, totalVolume, valueAreaPct)
	return volumeProfileResult{
		poc:       rangeLow + bucketSize*float64(maxIndex),
		vah:       rangeLow + bucketSize*float64(valueAreaUp),
		val:       rangeLow + bucketSize*float64(valueAreaDown),
		rangeHigh: rangeHigh,
		rangeLow:  rangeLow,
	}, true
}

func supplyDemandRange(
	highs []float64,
	lows []float64,
	closes []float64,
	volumes []float64,
	lookback int,
	bins int,
	thresholdPct float64,
) (supplyDemandRangeResult, bool) {
	if lookback <= 0 || bins < 2 || thresholdPct <= 0 || len(closes) < lookback ||
		len(highs) != len(closes) || len(lows) != len(closes) || len(volumes) != len(closes) {
		return supplyDemandRangeResult{}, false
	}
	start := len(closes) - lookback
	rangeHigh := highs[start]
	rangeLow := lows[start]
	for index := start + 1; index < len(closes); index++ {
		rangeHigh = math.Max(rangeHigh, highs[index])
		rangeLow = math.Min(rangeLow, lows[index])
	}
	if rangeHigh <= rangeLow {
		return supplyDemandRangeResult{}, false
	}
	bucketSize := (rangeHigh - rangeLow) / float64(bins)
	if bucketSize <= 0 {
		return supplyDemandRangeResult{}, false
	}
	bucketVolumes := make([]float64, bins)
	for index := start; index < len(closes); index++ {
		lowBucket := supplyDemandBucketIndex(lows[index], rangeLow, bucketSize, bins)
		highBucket := supplyDemandBucketIndex(highs[index], rangeLow, bucketSize, bins)
		if highBucket < lowBucket {
			continue
		}
		coveredBuckets := highBucket - lowBucket + 1
		volumePerBucket := volumes[index] / float64(coveredBuckets)
		for bucket := lowBucket; bucket <= highBucket; bucket++ {
			bucketVolumes[bucket] += volumePerBucket
		}
	}
	totalVolume := sum(bucketVolumes)
	if totalVolume == 0 {
		return supplyDemandRangeResult{}, false
	}
	targetVolume := totalVolume * thresholdPct / 100
	supplyIndex, supplyWAvg, okSupply := supplyDemandBoundary(bucketVolumes, rangeLow, bucketSize, targetVolume, true)
	demandIndex, demandWAvg, okDemand := supplyDemandBoundary(bucketVolumes, rangeLow, bucketSize, targetVolume, false)
	if !okSupply || !okDemand {
		return supplyDemandRangeResult{}, false
	}
	result := supplyDemandRangeResult{
		supplyTop:    rangeHigh,
		supplyBottom: rangeLow + bucketSize*float64(supplyIndex),
		demandTop:    rangeLow + bucketSize*float64(demandIndex+1),
		demandBottom: rangeLow,
		supplyWAvg:   supplyWAvg,
		demandWAvg:   demandWAvg,
	}
	result.supplyAvg = (result.supplyTop + result.supplyBottom) / 2
	result.demandAvg = (result.demandTop + result.demandBottom) / 2
	result.equilibrium = (rangeHigh + rangeLow) / 2
	result.weightedEquilibrium = (result.supplyWAvg + result.demandWAvg) / 2
	return result, true
}

func supplyDemandBoundary(bucketVolumes []float64, rangeLow float64, bucketSize float64, targetVolume float64, fromHigh bool) (int, float64, bool) {
	if len(bucketVolumes) == 0 || targetVolume <= 0 {
		return 0, 0, false
	}
	var volumeSum float64
	var weightedSum float64
	if fromHigh {
		for index := len(bucketVolumes) - 1; index >= 0; index-- {
			center := rangeLow + bucketSize*(float64(index)+0.5)
			volume := bucketVolumes[index]
			volumeSum += volume
			weightedSum += center * volume
			if volumeSum >= targetVolume {
				return index, weightedSum / volumeSum, true
			}
		}
		return 0, weightedSum / volumeSum, volumeSum > 0
	}
	for index, volume := range bucketVolumes {
		center := rangeLow + bucketSize*(float64(index)+0.5)
		volumeSum += volume
		weightedSum += center * volume
		if volumeSum >= targetVolume {
			return index, weightedSum / volumeSum, true
		}
	}
	return len(bucketVolumes) - 1, weightedSum / volumeSum, volumeSum > 0
}

func supplyDemandBucketIndex(price float64, rangeLow float64, bucketSize float64, bins int) int {
	index := int(math.Floor((price - rangeLow) / bucketSize))
	return clampInt(index, 0, bins-1)
}

func supplyDemandPosition(price float64, zone supplyDemandRangeResult) string {
	switch {
	case price > zone.supplyTop:
		return "above_supply"
	case price >= zone.supplyBottom:
		return "in_supply"
	case price <= zone.demandBottom:
		return "below_demand"
	case price <= zone.demandTop:
		return "in_demand"
	default:
		return "between_zones"
	}
}

func volumeProfileBucketIndex(price float64, rangeLow float64, bucketSize float64, bins int) int {
	index := int(math.Floor((price - rangeLow) / bucketSize))
	return clampInt(index, 0, bins-1)
}

func clampInt(value int, minimum int, maximum int) int {
	switch {
	case value < minimum:
		return minimum
	case value > maximum:
		return maximum
	default:
		return value
	}
}

func volumeProfileValueArea(bucketVolumes []float64, maxIndex int, totalVolume float64, valueAreaPct float64) (int, int) {
	targetVolume := totalVolume * valueAreaPct / 100
	up := maxIndex
	down := maxIndex
	valueAreaVolume := bucketVolumes[maxIndex]
	for valueAreaVolume < targetVolume {
		upVolume := 0.0
		if up < len(bucketVolumes)-1 {
			upVolume = bucketVolumes[up+1]
		}
		downVolume := 0.0
		if down > 0 {
			downVolume = bucketVolumes[down-1]
		}
		if upVolume == 0 && downVolume == 0 {
			break
		}
		if upVolume >= downVolume {
			valueAreaVolume += upVolume
			up++
		} else {
			valueAreaVolume += downVolume
			down--
		}
	}
	return down, up
}

func volumeProfilePosition(price float64, vah float64, val float64) string {
	switch {
	case price > vah:
		return "above_value_area"
	case price < val:
		return "below_value_area"
	default:
		return "inside_value_area"
	}
}

func volumeProfilePOCSide(price float64, poc float64) string {
	const threshold = 0.00000001
	switch {
	case price > poc+threshold:
		return "above"
	case price < poc-threshold:
		return "below"
	default:
		return "at"
	}
}

func volumeProfileValueAreaState(price float64, vah float64, val float64) string {
	switch {
	case price > vah:
		return "upper_breakout"
	case price < val:
		return "lower_breakdown"
	default:
		return "balanced"
	}
}

func zScore(values []float64, period int) (float64, bool) {
	if period <= 1 || len(values) < period {
		return 0, false
	}
	window := values[len(values)-period:]
	mean := sum(window) / float64(period)
	var variance float64
	for _, value := range window {
		diff := value - mean
		variance += diff * diff
	}
	stddev := math.Sqrt(variance / float64(period))
	if stddev == 0 {
		return 0, true
	}
	return (values[len(values)-1] - mean) / stddev, true
}

func standardDeviationRing(values []float64, endIndex int, period int) float64 {
	if period <= 0 || len(values) < period {
		return 0
	}
	start := endIndex - period + 1
	var sum float64
	for index := start; index <= endIndex; index++ {
		sum += values[index%len(values)]
	}
	mean := sum / float64(period)
	var variance float64
	for index := start; index <= endIndex; index++ {
		diff := values[index%len(values)] - mean
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(period))
}

func slope(values []float64, period int) float64 {
	if period <= 1 || len(values) < period {
		return 0
	}
	window := values[len(values)-period:]
	return window[len(window)-1] - window[0]
}

func moneyFlowSignal(mfi float64, pressure float64) string {
	switch {
	case mfi >= 60 && pressure > 0.15:
		return "inflow"
	case mfi <= 40 && pressure < -0.15:
		return "outflow"
	default:
		return "neutral"
	}
}

func vfiState(value float64) string {
	switch {
	case value > 0:
		return "inflow"
	case value < 0:
		return "outflow"
	default:
		return "neutral"
	}
}

func vfiMomentum(hist float64) string {
	switch {
	case hist > 0:
		return "rising"
	case hist < 0:
		return "falling"
	default:
		return "flat"
	}
}

func volumeState(zscore float64, ok bool) string {
	if !ok {
		return "normal"
	}
	switch {
	case zscore >= 3:
		return "climax"
	case zscore >= 2:
		return "spike"
	case zscore <= -1.8:
		return "dry_up"
	case zscore <= -1:
		return "dry"
	default:
		return "normal"
	}
}

func cmfState(value float64, ok bool) string {
	if !ok {
		return "neutral"
	}
	switch {
	case value >= 0.05:
		return "inflow"
	case value <= -0.05:
		return "outflow"
	default:
		return "neutral"
	}
}

func priceVolumeAction(closes []float64, volumes []float64, volumeRatio5 float64, ok bool) string {
	if !ok || len(closes) < 6 || len(volumes) < 6 {
		return "neutral"
	}
	priceChange := percentDistance(closes[len(closes)-1], closes[len(closes)-2])
	fiveBarChange := percentDistance(closes[len(closes)-1], closes[len(closes)-6])
	switch {
	case volumeRatio5 >= 1.8 && priceChange > 0:
		return "volume_expansion_up"
	case volumeRatio5 >= 1.8 && priceChange < 0:
		return "volume_expansion_down"
	case volumeRatio5 <= 0.7 && fiveBarChange < 0:
		return "volume_shrink_pullback"
	default:
		return "neutral"
	}
}

func breakoutVolumeConfirm(highs []float64, closes []float64, volumeBreakoutRatio float64, ok bool) string {
	if !ok || len(closes) < 21 {
		return "none"
	}
	previousHigh, _ := highLow(highs[len(highs)-21:len(highs)-1], highs[len(highs)-21:len(highs)-1])
	last := len(closes) - 1
	if closes[last] > previousHigh {
		if volumeBreakoutRatio >= 0.9 {
			return "confirm"
		}
		return "failed"
	}
	return "none"
}

func breakoutVolumeStrength(volumeBreakoutRatio float64, ok bool) string {
	if !ok {
		return "none"
	}
	switch {
	case volumeBreakoutRatio >= 1.5:
		return "strong"
	case volumeBreakoutRatio >= 0.9:
		return "weak"
	default:
		return "none"
	}
}

func volumeDivergence(closes []float64, volumes []float64, period int) string {
	score := volumeDivergenceScore(closes, volumes, period)
	switch {
	case score > 0:
		return "bearish"
	case score < 0:
		return "bullish"
	default:
		return "none"
	}
}

func volumeDivergenceScore(closes []float64, volumes []float64, period int) float64 {
	if period <= 0 || len(closes) < period || len(volumes) != len(closes) {
		return 0
	}
	start := len(closes) - period
	priceHigh, priceLow := highLow(closes[start:], closes[start:])
	volumeHigh, volumeLow := highLow(volumes[start:], volumes[start:])
	last := len(closes) - 1
	switch {
	case closes[last] >= priceHigh && volumes[last] < volumeHigh*0.8:
		return 1
	case closes[last] <= priceLow && volumes[last] > volumeLow*1.2:
		return -1
	default:
		return 0
	}
}

func volumePhase(pressure float64, cmf float64, ok bool) string {
	if !ok {
		return "neutral"
	}
	switch {
	case pressure > 0.15 && cmf > 0.05:
		return "accumulation"
	case pressure < -0.15 && cmf < -0.05:
		return "distribution"
	default:
		return "neutral"
	}
}

func priceVolumeConfirmation(closes []float64, obvValues []float64, pvtValues []float64) string {
	if len(closes) < 20 || len(obvValues) != len(closes) || len(pvtValues) != len(closes) {
		return "neutral"
	}
	priceSlope := slope(closes, 5)
	obvSlope := slope(obvValues, 5)
	pvtSlope := slope(pvtValues, 5)
	return priceVolumeConfirmationFromSlopes(closes, obvSlope, pvtSlope, priceSlope)
}

func priceVolumeConfirmationFromSlopes(closes []float64, obvSlope float64, pvtSlope float64, priceSlope ...float64) string {
	if len(closes) < 20 {
		return "neutral"
	}
	currentPriceSlope := 0.0
	if len(priceSlope) > 0 {
		currentPriceSlope = priceSlope[0]
	} else {
		currentPriceSlope = slope(closes, 5)
	}
	last := len(closes) - 1
	recentHigh, recentLow := highLow(closes[last-19:last], closes[last-19:last])
	switch {
	case closes[last] > recentHigh && (obvSlope < 0 || pvtSlope < 0):
		return "divergence_bear"
	case closes[last] < recentLow && (obvSlope > 0 || pvtSlope > 0):
		return "divergence_bull"
	case currentPriceSlope > 0 && obvSlope > 0 && pvtSlope > 0:
		return "confirm_up"
	case currentPriceSlope < 0 && obvSlope < 0 && pvtSlope < 0:
		return "confirm_down"
	default:
		return "neutral"
	}
}

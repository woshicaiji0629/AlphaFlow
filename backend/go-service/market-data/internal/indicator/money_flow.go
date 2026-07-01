package indicator

import "math"

func addMoneyFlowFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64) {
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

	obvSeries := obvSeries(closes, volumes)
	obvSlope := slope(obvSeries, 5)
	setValue(values, "obv_slope5", obvSlope, len(obvSeries) >= 5)

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

	pvtSeries := priceVolumeTrendSeries(closes, volumes)
	pvt := pvtSeries[len(pvtSeries)-1]
	setValue(values, "price_volume_trend", pvt, true)
	cmfValue, ok := chaikinMoneyFlow(highs, lows, closes, volumes, 20)
	setValue(values, "cmf20", cmfValue, ok)
	adValues := accumulationDistributionSeries(highs, lows, closes, volumes)
	setValue(values, "ad_line", adValues[len(adValues)-1], len(adValues) > 0)
	setValue(values, "ad_line_slope5", slope(adValues, 5), len(adValues) >= 5)

	signals["money_flow"] = moneyFlowSignal(mfi, pressure)
	signals["volume_state"] = volumeState(volumeZScore, ok)
	signals["price_volume_confirmation"] = priceVolumeConfirmation(closes, obvSeries, pvtSeries)
	signals["cmf_state"] = cmfState(cmfValue, ok)
	signals["price_volume_action"] = priceVolumeAction(closes, volumes, volumeRatio5, ok5)
	signals["breakout_volume_confirm"] = breakoutVolumeConfirm(highs, closes, volumeBreakoutRatio, okBreakout)
	signals["breakout_volume_strength"] = breakoutVolumeStrength(volumeBreakoutRatio, okBreakout)
	signals["volume_divergence"] = volumeDivergence(closes, volumes, 20)
	signals["volume_phase"] = volumePhase(pressure, cmfValue, ok)
	addVolumeProfileFeatures(values, signals, highs, lows, closes, volumes, 200, 100, 68)
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

type volumeProfileResult struct {
	poc       float64
	vah       float64
	val       float64
	rangeHigh float64
	rangeLow  float64
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
	last := len(closes) - 1
	recentHigh, recentLow := highLow(closes[last-19:last], closes[last-19:last])
	switch {
	case closes[last] > recentHigh && (obvSlope < 0 || pvtSlope < 0):
		return "divergence_bear"
	case closes[last] < recentLow && (obvSlope > 0 || pvtSlope > 0):
		return "divergence_bull"
	case priceSlope > 0 && obvSlope > 0 && pvtSlope > 0:
		return "confirm_up"
	case priceSlope < 0 && obvSlope < 0 && pvtSlope < 0:
		return "confirm_down"
	default:
		return "neutral"
	}
}

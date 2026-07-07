package indicatorcalc

import "math"

func addTradingViewFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	addQQEModFeatures(values, signals, closes, 6, 5, 3)
	addQQEModEnhancedFeatures(values, signals, closes)
	addUTBotFeatures(values, signals, highs, lows, closes, 10, 1)
	addSSLChannelFeatures(values, signals, highs, lows, closes, 10)
	addRangeFilterFeatures(values, signals, closes, 100, 3)
	addWilliamsVixFixFeatures(values, signals, highs, lows, closes, 22, 20, 2, 50, 0.85)
	addTDSequentialFeatures(values, signals, closes)
	addNadarayaWatsonEnvelopeFeatures(values, signals, closes, 50, 8, 3)
}

func addQQEModFeatures(values map[string]string, signals map[string]string, closes []float64, rsiPeriod int, smoothing int, factor float64) {
	line, signal, previousLine, previousSignal, ok := qqeMod(closes, rsiPeriod, smoothing, factor)
	if !ok {
		return
	}
	setValue(values, "qqe_line", line, true)
	setValue(values, "qqe_signal", signal, true)
	setValue(values, "qqe_hist", line-signal, true)
	signals["qqe_trend"] = thresholdTrend(line, signal, 50)
	signals["qqe_cross"] = crossSignal(previousLine, previousSignal, line, signal)
}

func addQQEModEnhancedFeatures(values map[string]string, signals map[string]string, closes []float64) {
	result, ok := qqeModEnhanced(closes, 6, 5, 3, 1.61, 50, 0.35, 3)
	if !ok {
		return
	}
	setValue(values, "qqe_primary_line", result.primaryLine, true)
	setValue(values, "qqe_primary_trend", result.primaryTrend, true)
	setValue(values, "qqe_secondary_line", result.secondaryLine, true)
	setValue(values, "qqe_secondary_trend", result.secondaryTrend, true)
	setValue(values, "qqe_bb_upper", result.bbUpper, true)
	setValue(values, "qqe_bb_lower", result.bbLower, true)
	setValue(values, "qqe_primary_hist", result.primaryHist, true)
	setValue(values, "qqe_secondary_hist", result.secondaryHist, true)
	signals["qqe_mod_signal"] = result.signal
	signals["qqe_primary_zero_cross"] = result.zeroCross
}

type qqeModEnhancedResult struct {
	primaryLine    float64
	primaryTrend   float64
	secondaryLine  float64
	secondaryTrend float64
	bbUpper        float64
	bbLower        float64
	primaryHist    float64
	secondaryHist  float64
	signal         string
	zeroCross      string
}

func qqeModEnhanced(
	closes []float64,
	rsiPeriod int,
	smoothing int,
	primaryFactor float64,
	secondaryFactor float64,
	bbPeriod int,
	bbMultiplier float64,
	secondaryThreshold float64,
) (qqeModEnhancedResult, bool) {
	primaryTrend, primaryLine, okPrimary := qqeModTrendSeries(closes, rsiPeriod, smoothing, primaryFactor)
	secondaryTrend, secondaryLine, okSecondary := qqeModTrendSeries(closes, rsiPeriod, smoothing, secondaryFactor)
	if !okPrimary || !okSecondary || len(primaryTrend) < bbPeriod || len(primaryLine) < 2 || len(secondaryLine) == 0 {
		return qqeModEnhancedResult{}, false
	}
	primaryTrendHist := make([]float64, 0, len(primaryTrend))
	for _, value := range primaryTrend {
		primaryTrendHist = append(primaryTrendHist, value-50)
	}
	basis, ok := sma(primaryTrendHist, bbPeriod)
	if !ok {
		return qqeModEnhancedResult{}, false
	}
	deviation, ok := standardDeviation(primaryTrendHist, bbPeriod)
	if !ok {
		return qqeModEnhancedResult{}, false
	}
	lastPrimary := primaryLine[len(primaryLine)-1]
	previousPrimary := primaryLine[len(primaryLine)-2]
	lastSecondary := secondaryLine[len(secondaryLine)-1]
	lastPrimaryHist := lastPrimary - 50
	lastSecondaryHist := lastSecondary - 50
	upper := basis + deviation*bbMultiplier
	lower := basis - deviation*bbMultiplier
	result := qqeModEnhancedResult{
		primaryLine:    lastPrimary,
		primaryTrend:   primaryTrend[len(primaryTrend)-1],
		secondaryLine:  lastSecondary,
		secondaryTrend: secondaryTrend[len(secondaryTrend)-1],
		bbUpper:        upper,
		bbLower:        lower,
		primaryHist:    lastPrimaryHist,
		secondaryHist:  lastSecondaryHist,
		signal:         qqeModSignal(lastPrimaryHist, lastSecondaryHist, upper, lower, secondaryThreshold),
		zeroCross:      qqeZeroCross(previousPrimary, lastPrimary),
	}
	return result, true
}

func qqeModTrendSeries(closes []float64, rsiPeriod int, smoothing int, factor float64) ([]float64, []float64, bool) {
	rsiValues, ok := rsiSeries(closes, rsiPeriod)
	if !ok || len(rsiValues) < smoothing*3 || factor <= 0 {
		return nil, nil, false
	}
	smoothed, ok := emaSeries(rsiValues, smoothing)
	if !ok || len(smoothed) < 3 {
		return nil, nil, false
	}
	deltas := make([]float64, 0, len(smoothed)-1)
	for index := 1; index < len(smoothed); index++ {
		deltas = append(deltas, math.Abs(smoothed[index]-smoothed[index-1]))
	}
	wildersPeriod := rsiPeriod*2 - 1
	smoothedDeltas, ok := emaSeries(deltas, wildersPeriod)
	if !ok || len(smoothedDeltas) < 2 {
		return nil, nil, false
	}
	offset := len(smoothed) - len(smoothedDeltas)
	if offset <= 0 {
		return nil, nil, false
	}
	longBand := smoothed[offset] - smoothedDeltas[0]*factor
	shortBand := smoothed[offset] + smoothedDeltas[0]*factor
	trendDirection := 1
	trendValues := make([]float64, 0, len(smoothedDeltas))
	lineValues := make([]float64, 0, len(smoothedDeltas))
	for index := offset; index < len(smoothed); index++ {
		rangeValue := smoothedDeltas[index-offset] * factor
		line := smoothed[index]
		previousLine := line
		if index > 0 {
			previousLine = smoothed[index-1]
		}
		newLongBand := line - rangeValue
		newShortBand := line + rangeValue
		previousLongBand := longBand
		previousShortBand := shortBand
		if previousLine > previousLongBand && line > previousLongBand {
			longBand = math.Max(previousLongBand, newLongBand)
		} else {
			longBand = newLongBand
		}
		if previousLine < previousShortBand && line < previousShortBand {
			shortBand = math.Min(previousShortBand, newShortBand)
		} else {
			shortBand = newShortBand
		}
		if crossesAbove(previousLine, line, previousShortBand) {
			trendDirection = 1
		} else if crossesBelow(previousLine, line, previousLongBand) {
			trendDirection = -1
		}
		if trendDirection == 1 {
			trendValues = append(trendValues, longBand)
		} else {
			trendValues = append(trendValues, shortBand)
		}
		lineValues = append(lineValues, line)
	}
	if len(trendValues) < 2 || len(lineValues) < 2 {
		return nil, nil, false
	}
	return trendValues, lineValues, true
}

func qqeModSignal(primaryHist float64, secondaryHist float64, upper float64, lower float64, threshold float64) string {
	switch {
	case secondaryHist > threshold && primaryHist > upper:
		return "up"
	case secondaryHist < -threshold && primaryHist < lower:
		return "down"
	default:
		return "none"
	}
}

func qqeZeroCross(previous float64, current float64) string {
	switch {
	case previous <= 50 && current > 50:
		return "up"
	case previous >= 50 && current < 50:
		return "down"
	default:
		return "none"
	}
}

func crossesAbove(previousValue float64, currentValue float64, previousLevel float64) bool {
	return previousValue <= previousLevel && currentValue > previousLevel
}

func crossesBelow(previousValue float64, currentValue float64, previousLevel float64) bool {
	return previousValue >= previousLevel && currentValue < previousLevel
}

func qqeMod(closes []float64, rsiPeriod int, smoothing int, factor float64) (float64, float64, float64, float64, bool) {
	rsiValues, ok := rsiSeries(closes, rsiPeriod)
	if !ok || len(rsiValues) < smoothing*3 || factor <= 0 {
		return 0, 0, 0, 0, false
	}
	smoothed, ok := emaSeries(rsiValues, smoothing)
	if !ok || len(smoothed) < smoothing+2 {
		return 0, 0, 0, 0, false
	}
	deltas := make([]float64, 0, len(smoothed)-1)
	for index := 1; index < len(smoothed); index++ {
		deltas = append(deltas, math.Abs(smoothed[index]-smoothed[index-1]))
	}
	wildersPeriod := rsiPeriod*2 - 1
	smoothedDeltas, ok := emaSeries(deltas, wildersPeriod)
	if !ok {
		return 0, 0, 0, 0, false
	}
	dynamicRange, ok := emaSeries(smoothedDeltas, wildersPeriod)
	if !ok || len(dynamicRange) < 2 {
		return 0, 0, 0, 0, false
	}
	offset := len(smoothed) - len(dynamicRange)
	if offset <= 0 {
		return 0, 0, 0, 0, false
	}
	longBand := smoothed[offset] - dynamicRange[0]*factor
	shortBand := smoothed[offset] + dynamicRange[0]*factor
	trend := 1
	trailing := longBand
	previousTrailing := trailing
	for index := offset + 1; index < len(smoothed); index++ {
		rangeValue := dynamicRange[index-offset] * factor
		line := smoothed[index]
		previousLine := smoothed[index-1]
		newLongBand := line - rangeValue
		newShortBand := line + rangeValue
		if previousLine > longBand && line > longBand {
			longBand = math.Max(longBand, newLongBand)
		} else {
			longBand = newLongBand
		}
		if previousLine < shortBand && line < shortBand {
			shortBand = math.Min(shortBand, newShortBand)
		} else {
			shortBand = newShortBand
		}
		if line > shortBand {
			trend = 1
		} else if line < longBand {
			trend = -1
		}
		previousTrailing = trailing
		if trend == 1 {
			trailing = longBand
		} else {
			trailing = shortBand
		}
	}
	return smoothed[len(smoothed)-1], trailing, smoothed[len(smoothed)-2], previousTrailing, true
}

func addUTBotFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64) {
	stop, direction, previousDirection, ok := utBot(highs, lows, closes, period, multiplier)
	if !ok {
		return
	}
	setValue(values, "ut_stop", stop, true)
	setValue(values, "ut_stop_distance_pct", absFloat(percentDistance(closes[len(closes)-1], stop)), stop != 0)
	signals["ut_direction"] = direction
	signals["ut_signal"] = directionFlipSignal(previousDirection, direction)
}

func utBot(highs []float64, lows []float64, closes []float64, period int, multiplier float64) (float64, string, string, bool) {
	atrValues, ok := atrSeries(highs, lows, closes, period)
	if !ok || len(atrValues) < 2 {
		return 0, "", "", false
	}
	stop := closes[period] - multiplier*atrValues[0]
	direction := "up"
	previousDirection := direction
	for index := period + 1; index < len(closes); index++ {
		previousStop := stop
		previousDirection = direction
		loss := multiplier * atrValues[index-period]
		if closes[index] > previousStop && closes[index-1] > previousStop {
			stop = math.Max(previousStop, closes[index]-loss)
			direction = "up"
			continue
		}
		if closes[index] < previousStop && closes[index-1] < previousStop {
			stop = math.Min(previousStop, closes[index]+loss)
			direction = "down"
			continue
		}
		if closes[index] > previousStop {
			stop = closes[index] - loss
			direction = "up"
		} else {
			stop = closes[index] + loss
			direction = "down"
		}
	}
	return stop, direction, previousDirection, true
}

func addSSLChannelFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int) {
	upper, lower, direction, previousDirection, ok := sslChannel(highs, lows, closes, period)
	if !ok {
		return
	}
	setValue(values, "ssl_upper", upper, true)
	setValue(values, "ssl_lower", lower, true)
	setValue(values, "ssl_width_pct", (upper-lower)/closes[len(closes)-1]*100, closes[len(closes)-1] != 0)
	signals["ssl_direction"] = direction
	signals["ssl_cross"] = directionFlipCross(previousDirection, direction)
}

func sslChannel(highs []float64, lows []float64, closes []float64, period int) (float64, float64, string, string, bool) {
	if period <= 0 || len(closes) < period+1 || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, 0, "", "", false
	}
	direction := "neutral"
	previousDirection := direction
	var upper float64
	var lower float64
	for end := period; end <= len(closes); end++ {
		highMA, okHigh := sma(highs[:end], period)
		lowMA, okLow := sma(lows[:end], period)
		if !okHigh || !okLow {
			return 0, 0, "", "", false
		}
		previousDirection = direction
		switch {
		case closes[end-1] > highMA:
			direction = "bull"
		case closes[end-1] < lowMA:
			direction = "bear"
		}
		if direction == "bear" {
			upper = lowMA
			lower = highMA
		} else {
			upper = highMA
			lower = lowMA
		}
	}
	return upper, lower, direction, previousDirection, true
}

func addRangeFilterFeatures(values map[string]string, signals map[string]string, closes []float64, period int, multiplier float64) {
	filter, upper, lower, direction, ok := rangeFilterCompact(closes, period, multiplier)
	if !ok {
		filter, upper, lower, direction, ok = rangeFilter(closes, period, multiplier)
	}
	if !ok {
		return
	}
	setValue(values, "range_filter", filter, true)
	setValue(values, "range_filter_upper", upper, true)
	setValue(values, "range_filter_lower", lower, true)
	setValue(values, "range_filter_distance_pct", percentDistance(closes[len(closes)-1], filter), filter != 0)
	signals["range_filter_direction"] = direction
}

func rangeFilterCompact(closes []float64, period int, multiplier float64) (float64, float64, float64, string, bool) {
	if period <= 0 || multiplier <= 0 || len(closes) < period+2 {
		return 0, 0, 0, "", false
	}
	rangeEMA := newStreamEMAState(period)
	filter := closes[period]
	direction := "flat"
	for index := 1; index < len(closes); index++ {
		rangeEMA.append(math.Abs(closes[index] - closes[index-1]))
		if index <= period || !rangeEMA.ready {
			continue
		}
		smoothRange := rangeEMA.value * multiplier
		previous := filter
		switch {
		case closes[index] > previous:
			filter = math.Max(previous, closes[index]-smoothRange)
		case closes[index] < previous:
			filter = math.Min(previous, closes[index]+smoothRange)
		}
		switch {
		case filter > previous:
			direction = "up"
		case filter < previous:
			direction = "down"
		}
	}
	smoothRange := rangeEMA.value * multiplier
	return filter, filter + smoothRange, filter - smoothRange, direction, true
}

func rangeFilter(closes []float64, period int, multiplier float64) (float64, float64, float64, string, bool) {
	if period <= 0 || multiplier <= 0 || len(closes) < period+2 {
		return 0, 0, 0, "", false
	}
	ranges := make([]float64, 0, len(closes)-1)
	for index := 1; index < len(closes); index++ {
		ranges = append(ranges, math.Abs(closes[index]-closes[index-1]))
	}
	smoothRangeSeries, ok := emaSeries(ranges, period)
	if !ok || len(smoothRangeSeries) == 0 {
		return 0, 0, 0, "", false
	}
	filter := closes[period]
	direction := "flat"
	for index := period + 1; index < len(closes); index++ {
		rangeIndex := index - period
		if rangeIndex >= len(smoothRangeSeries) {
			rangeIndex = len(smoothRangeSeries) - 1
		}
		smoothRange := smoothRangeSeries[rangeIndex] * multiplier
		previous := filter
		switch {
		case closes[index] > previous:
			filter = math.Max(previous, closes[index]-smoothRange)
		case closes[index] < previous:
			filter = math.Min(previous, closes[index]+smoothRange)
		}
		switch {
		case filter > previous:
			direction = "up"
		case filter < previous:
			direction = "down"
		}
	}
	smoothRange := smoothRangeSeries[len(smoothRangeSeries)-1] * multiplier
	return filter, filter + smoothRange, filter - smoothRange, direction, true
}

func addWilliamsVixFixFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, bbLength int, bbMultiplier float64, lookback int, percentileHigh float64) {
	result, ok := williamsVixFixCompact(lows, closes, period, bbLength, bbMultiplier, lookback, percentileHigh)
	if !ok {
		result, ok = williamsVixFix(lows, closes, period, bbLength, bbMultiplier, lookback, percentileHigh)
	}
	if !ok {
		return
	}
	setValue(values, "wvf", result.value, true)
	setValue(values, "wvf_mid_line", result.mid, true)
	setValue(values, "wvf_upper_band", result.upperBand, true)
	setValue(values, "wvf_lower_band", result.lowerBand, true)
	setValue(values, "wvf_range_high", result.rangeHigh, true)
	setValue(values, "wvf_range_low", result.rangeLow, true)
	signals["wvf_state"] = williamsVixFixState(result.value, result.upperBand, result.rangeHigh)
	signals["wvf_zone"] = williamsVixFixZone(result.value, result.upperBand, result.lowerBand, result.rangeHigh, result.rangeLow)
}

type williamsVixFixResult struct {
	value     float64
	mid       float64
	upperBand float64
	lowerBand float64
	rangeHigh float64
	rangeLow  float64
}

func williamsVixFixCompact(lows []float64, closes []float64, period int, bbLength int, bbMultiplier float64, lookback int, percentileHigh float64) (williamsVixFixResult, bool) {
	if period <= 0 || bbLength <= 0 || lookback <= 0 || len(closes) < period || len(lows) != len(closes) {
		return williamsVixFixResult{}, false
	}
	seriesCount := 0
	keep := lookback
	if bbLength > keep {
		keep = bbLength
	}
	recent := make([]float64, keep)
	closeHighs := newFloatMonotonicWindow(true)
	if !closeHighs.canHold(period) {
		return williamsVixFixResult{}, false
	}
	for index, closeValue := range closes {
		closeHighs.push(index, closeValue)
		closeHighs.expireBefore(index - period + 1)
		if index+1 < period {
			continue
		}
		highestClose, ok := closeHighs.value()
		if !ok {
			return williamsVixFixResult{}, false
		}
		value := 0.0
		if highestClose != 0 {
			value = (highestClose - lows[index]) / highestClose * 100
		}
		slot := seriesCount % keep
		recent[slot] = value
		seriesCount++
	}
	if seriesCount < bbLength || seriesCount < lookback {
		return williamsVixFixResult{}, false
	}
	last := recent[(seriesCount-1)%keep]
	mid := ringTailSum(recent, seriesCount, bbLength) / float64(bbLength)
	deviation := ringTailStandardDeviation(recent, seriesCount, bbLength, mid)
	rangeHigh, rangeLow := ringTailHighLow(recent, seriesCount, lookback)
	rangeHigh *= percentileHigh
	rangeLow *= 1.01
	return williamsVixFixResult{
		value:     last,
		mid:       mid,
		upperBand: mid + bbMultiplier*deviation,
		lowerBand: mid - bbMultiplier*deviation,
		rangeHigh: rangeHigh,
		rangeLow:  rangeLow,
	}, true
}

func williamsVixFix(lows []float64, closes []float64, period int, bbLength int, bbMultiplier float64, lookback int, percentileHigh float64) (williamsVixFixResult, bool) {
	series, ok := williamsVixFixSeries(lows, closes, period)
	if !ok || len(series) < bbLength || len(series) < lookback {
		return williamsVixFixResult{}, false
	}
	last := series[len(series)-1]
	mid, _ := sma(series, bbLength)
	deviation, _ := standardDeviation(series, bbLength)
	upperBand := mid + bbMultiplier*deviation
	lowerBand := mid - bbMultiplier*deviation
	rangeHigh := highestValue(series[len(series)-lookback:]) * percentileHigh
	rangeLow := lowestValue(series[len(series)-lookback:]) * 1.01
	return williamsVixFixResult{
		value:     last,
		mid:       mid,
		upperBand: upperBand,
		lowerBand: lowerBand,
		rangeHigh: rangeHigh,
		rangeLow:  rangeLow,
	}, true
}

func williamsVixFixSeries(lows []float64, closes []float64, period int) ([]float64, bool) {
	if period <= 0 || len(closes) < period || len(lows) != len(closes) {
		return nil, false
	}
	result := make([]float64, 0, len(closes)-period+1)
	for end := period; end <= len(closes); end++ {
		highestClose := highestValue(closes[end-period : end])
		if highestClose == 0 {
			result = append(result, 0)
			continue
		}
		result = append(result, (highestClose-lows[end-1])/highestClose*100)
	}
	return result, len(result) > 0
}

func williamsVixFixState(value float64, upperBand float64, rangeHigh float64) string {
	if value >= upperBand || value >= rangeHigh {
		return "panic"
	}
	return "normal"
}

func williamsVixFixZone(value float64, upperBand float64, lowerBand float64, rangeHigh float64, rangeLow float64) string {
	if value >= upperBand || value >= rangeHigh {
		return "panic"
	}
	if value <= lowerBand || value <= rangeLow {
		return "low_volatility"
	}
	return "normal"
}

func addTDSequentialFeatures(values map[string]string, signals map[string]string, closes []float64) {
	buyCount, sellCount, exhaustion := tdSequential(closes)
	setValue(values, "td_buy_setup_count", float64(buyCount), buyCount > 0)
	setValue(values, "td_sell_setup_count", float64(sellCount), sellCount > 0)
	signals["td_exhaustion"] = exhaustion
}

func tdSequential(closes []float64) (int, int, string) {
	if len(closes) < 5 {
		return 0, 0, "none"
	}
	buyCount := 0
	sellCount := 0
	for index := 4; index < len(closes); index++ {
		switch {
		case closes[index] < closes[index-4]:
			buyCount++
			sellCount = 0
		case closes[index] > closes[index-4]:
			sellCount++
			buyCount = 0
		default:
			buyCount = 0
			sellCount = 0
		}
		if buyCount > 9 {
			buyCount = 1
		}
		if sellCount > 9 {
			sellCount = 1
		}
	}
	switch {
	case buyCount == 9:
		return buyCount, sellCount, "buy"
	case sellCount == 9:
		return buyCount, sellCount, "sell"
	default:
		return buyCount, sellCount, "none"
	}
}

func addNadarayaWatsonEnvelopeFeatures(values map[string]string, signals map[string]string, closes []float64, length int, bandwidth float64, multiplier float64) {
	middle, mae, previousMiddle, ok := nadarayaWatsonEnvelope(closes, length, bandwidth)
	if !ok {
		return
	}
	upper := middle + mae*multiplier
	lower := middle - mae*multiplier
	last := closes[len(closes)-1]
	setValue(values, "nw_middle", middle, true)
	setValue(values, "nw_upper", upper, true)
	setValue(values, "nw_lower", lower, true)
	setValue(values, "nw_width_pct", (upper-lower)/middle*100, middle != 0)
	setValue(values, "nw_position", (last-lower)/(upper-lower), upper != lower)
	signals["nw_trend"] = slopeTrend(percentDistance(middle, previousMiddle))
	signals["nw_position_state"] = channelBreakout(last, upper, lower)
}

func nadarayaWatsonEnvelope(closes []float64, length int, bandwidth float64) (float64, float64, float64, bool) {
	if length <= 1 || bandwidth <= 0 || len(closes) < length+1 {
		return 0, 0, 0, false
	}
	var weightStorage [256]float64
	if length > len(weightStorage) {
		return nadarayaWatsonEnvelopeBatch(closes, length, bandwidth)
	}
	weights := weightStorage[:length]
	fillNadarayaWatsonWeights(weights, bandwidth)
	middle, ok := nadarayaWatsonAtWithWeights(closes, weights, len(closes))
	if !ok {
		return 0, 0, 0, false
	}
	previousMiddle, ok := nadarayaWatsonAtWithWeights(closes, weights, len(closes)-1)
	if !ok {
		return 0, 0, 0, false
	}
	var errorSum float64
	start := len(closes) - length
	for index := start; index < len(closes); index++ {
		fit, fitOK := nadarayaWatsonAtWithWeights(closes, weightsForNadarayaWatsonEnd(weights, index+1), index+1)
		if !fitOK {
			continue
		}
		errorSum += math.Abs(closes[index] - fit)
	}
	return middle, errorSum / float64(length), previousMiddle, true
}

func nadarayaWatsonEnvelopeBatch(closes []float64, length int, bandwidth float64) (float64, float64, float64, bool) {
	middle, ok := nadarayaWatsonAt(closes, length, bandwidth, len(closes))
	if !ok {
		return 0, 0, 0, false
	}
	previousMiddle, ok := nadarayaWatsonAt(closes, length, bandwidth, len(closes)-1)
	if !ok {
		return 0, 0, 0, false
	}
	var errorSum float64
	start := len(closes) - length
	for index := start; index < len(closes); index++ {
		fit, fitOK := nadarayaWatsonAt(closes[:index+1], minInt(length, index+1), bandwidth, index+1)
		if !fitOK {
			continue
		}
		errorSum += math.Abs(closes[index] - fit)
	}
	return middle, errorSum / float64(length), previousMiddle, true
}

func nadarayaWatsonAt(values []float64, length int, bandwidth float64, end int) (float64, bool) {
	if length <= 0 || end < length || end > len(values) {
		return 0, false
	}
	start := end - length
	var weighted float64
	var weightSum float64
	for index := start; index < end; index++ {
		distance := float64(end - 1 - index)
		weight := math.Exp(-(distance * distance) / (2 * bandwidth * bandwidth))
		weighted += values[index] * weight
		weightSum += weight
	}
	if weightSum == 0 {
		return 0, false
	}
	return weighted / weightSum, true
}

func fillNadarayaWatsonWeights(weights []float64, bandwidth float64) {
	for offset := range weights {
		distance := float64(len(weights) - 1 - offset)
		weights[offset] = math.Exp(-(distance * distance) / (2 * bandwidth * bandwidth))
	}
}

func weightsForNadarayaWatsonEnd(weights []float64, end int) []float64 {
	if end >= len(weights) {
		return weights
	}
	return weights[len(weights)-end:]
}

func nadarayaWatsonAtWithWeights(values []float64, weights []float64, end int) (float64, bool) {
	length := len(weights)
	if length <= 0 || end < length || end > len(values) {
		return 0, false
	}
	start := end - length
	var weighted float64
	var weightSum float64
	for index := start; index < end; index++ {
		weight := weights[index-start]
		weighted += values[index] * weight
		weightSum += weight
	}
	if weightSum == 0 {
		return 0, false
	}
	return weighted / weightSum, true
}

func thresholdTrend(value float64, signal float64, threshold float64) string {
	switch {
	case value > signal && value >= threshold:
		return "bull"
	case value < signal && value <= threshold:
		return "bear"
	default:
		return "neutral"
	}
}

func directionFlipSignal(previous string, current string) string {
	switch {
	case previous != current && current == "up":
		return "buy"
	case previous != current && current == "down":
		return "sell"
	default:
		return "none"
	}
}

func directionFlipCross(previous string, current string) string {
	switch {
	case previous != current && current == "bull":
		return "golden"
	case previous != current && current == "bear":
		return "dead"
	default:
		return "none"
	}
}

func slopeTrend(slopePct float64) string {
	switch {
	case slopePct > 0.05:
		return "up"
	case slopePct < -0.05:
		return "down"
	default:
		return "flat"
	}
}

func highestValue(values []float64) float64 {
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}

func lowestValue(values []float64) float64 {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func ringTailSum(values []float64, count int, length int) float64 {
	sum := 0.0
	start := count - length
	for index := start; index < count; index++ {
		sum += values[index%len(values)]
	}
	return sum
}

func ringTailStandardDeviation(values []float64, count int, length int, mean float64) float64 {
	if length <= 0 {
		return 0
	}
	var variance float64
	start := count - length
	for index := start; index < count; index++ {
		value := values[index%len(values)]
		diff := value - mean
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(length))
}

func ringTailHighLow(values []float64, count int, length int) (float64, float64) {
	start := count - length
	high := values[start%len(values)]
	low := high
	for index := start + 1; index < count; index++ {
		value := values[index%len(values)]
		if value > high {
			high = value
		}
		if value < low {
			low = value
		}
	}
	return high, low
}

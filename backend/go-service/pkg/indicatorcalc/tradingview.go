package indicatorcalc

import "math"

func addTradingViewFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	addQQEModFeatures(values, signals, closes, 6, 5, 3)
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
	filter, upper, lower, direction, ok := rangeFilter(closes, period, multiplier)
	if !ok {
		return
	}
	setValue(values, "range_filter", filter, true)
	setValue(values, "range_filter_upper", upper, true)
	setValue(values, "range_filter_lower", lower, true)
	setValue(values, "range_filter_distance_pct", percentDistance(closes[len(closes)-1], filter), filter != 0)
	signals["range_filter_direction"] = direction
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
	series, ok := williamsVixFixSeries(lows, closes, period)
	if !ok || len(series) < bbLength || len(series) < lookback {
		return
	}
	last := series[len(series)-1]
	mid, _ := sma(series, bbLength)
	deviation, _ := standardDeviation(series, bbLength)
	upperBand := mid + bbMultiplier*deviation
	rangeHigh := highestValue(series[len(series)-lookback:]) * percentileHigh
	setValue(values, "wvf", last, true)
	setValue(values, "wvf_upper_band", upperBand, true)
	setValue(values, "wvf_range_high", rangeHigh, true)
	signals["wvf_state"] = williamsVixFixState(last, upperBand, rangeHigh)
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

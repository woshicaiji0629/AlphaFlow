package indicatorcalc

import "math"

func addWilliamsVixFixFeatures(target *ValueSet, values map[string]string, signals map[string]string, lows []float64, closes []float64, period int, bbLength int, bbMultiplier float64, lookback int, percentileHigh float64) {
	result, ok := williamsVixFixCompact(lows, closes, period, bbLength, bbMultiplier, lookback, percentileHigh)
	if !ok {
		result, ok = williamsVixFix(lows, closes, period, bbLength, bbMultiplier, lookback, percentileHigh)
	}
	if !ok {
		return
	}
	setValueTarget(target, values, "wvf", result.value, true)
	setValueTarget(target, values, "wvf_mid_line", result.mid, true)
	setValueTarget(target, values, "wvf_upper_band", result.upperBand, true)
	setValueTarget(target, values, "wvf_lower_band", result.lowerBand, true)
	setValueTarget(target, values, "wvf_range_high", result.rangeHigh, true)
	setValueTarget(target, values, "wvf_range_low", result.rangeLow, true)
	signals["wvf_state"] = williamsVixFixState(result.value, result.upperBand, result.rangeHigh)
	signals["wvf_zone"] = williamsVixFixZone(result.value, result.upperBand, result.lowerBand, result.rangeHigh, result.rangeLow)
}

type williamsVixFixResult struct {
	value, mid, upperBand, lowerBand, rangeHigh, rangeLow float64
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
		value: last, mid: mid,
		upperBand: mid + bbMultiplier*deviation,
		lowerBand: mid - bbMultiplier*deviation,
		rangeHigh: rangeHigh, rangeLow: rangeLow,
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
		value: last, mid: mid, upperBand: upperBand, lowerBand: lowerBand,
		rangeHigh: rangeHigh, rangeLow: rangeLow,
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
		diff := values[index%len(values)] - mean
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(length))
}

func ringTailHighLow(values []float64, count int, length int) (float64, float64) {
	start := count - length
	high, low := values[start%len(values)], values[start%len(values)]
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

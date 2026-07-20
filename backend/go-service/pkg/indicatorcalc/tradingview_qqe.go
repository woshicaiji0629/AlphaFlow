package indicatorcalc

import "math"

func addQQEModFeaturesWithRSI(target *ValueSet, values map[string]string, signals map[string]string, rsiValues []float64, rsiOK bool, rsiPeriod int, smoothing int, factor float64) {
	smoothed, smoothedDeltas, _, foundationOK := qqeModTrendFoundation(rsiValues, rsiOK, rsiPeriod, smoothing)
	addQQEModFeaturesWithFoundation(target, values, signals, smoothed, smoothedDeltas, rsiPeriod, smoothing, factor, foundationOK)
}

func addQQEModFeaturesWithFoundation(target *ValueSet, values map[string]string, signals map[string]string, smoothed []float64, smoothedDeltas []float64, rsiPeriod int, smoothing int, factor float64, foundationOK bool) {
	line, signal, previousLine, previousSignal, ok := qqeModFromFoundation(smoothed, smoothedDeltas, rsiPeriod, smoothing, factor, foundationOK)
	if !ok {
		return
	}
	setValueTarget(target, values, "qqe_line", line, true)
	setValueTarget(target, values, "qqe_signal", signal, true)
	setValueTarget(target, values, "qqe_hist", line-signal, true)
	signals["qqe_trend"] = thresholdTrend(line, signal, 50)
	signals["qqe_cross"] = crossSignal(previousLine, previousSignal, line, signal)
}

func addQQEModEnhancedFeaturesWithRSI(target *ValueSet, values map[string]string, signals map[string]string, rsiValues []float64, rsiOK bool) {
	smoothed, smoothedDeltas, offset, foundationOK := qqeModTrendFoundation(rsiValues, rsiOK, 6, 5)
	addQQEModEnhancedFeaturesWithFoundation(target, values, signals, smoothed, smoothedDeltas, offset, foundationOK)
}

func addQQEModEnhancedFeaturesWithFoundation(target *ValueSet, values map[string]string, signals map[string]string, smoothed []float64, smoothedDeltas []float64, offset int, foundationOK bool) {
	result, ok := qqeModEnhancedFromFoundation(smoothed, smoothedDeltas, offset, 3, 1.61, 50, 0.35, 3, foundationOK)
	if !ok {
		return
	}
	setValueTarget(target, values, "qqe_primary_line", result.primaryLine, true)
	setValueTarget(target, values, "qqe_primary_trend", result.primaryTrend, true)
	setValueTarget(target, values, "qqe_secondary_line", result.secondaryLine, true)
	setValueTarget(target, values, "qqe_secondary_trend", result.secondaryTrend, true)
	setValueTarget(target, values, "qqe_bb_upper", result.bbUpper, true)
	setValueTarget(target, values, "qqe_bb_lower", result.bbLower, true)
	setValueTarget(target, values, "qqe_primary_hist", result.primaryHist, true)
	setValueTarget(target, values, "qqe_secondary_hist", result.secondaryHist, true)
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
	rsiValues, rsiOK := rsiSeries(closes, rsiPeriod)
	return qqeModEnhancedWithRSI(rsiValues, rsiOK, rsiPeriod, smoothing, primaryFactor, secondaryFactor, bbPeriod, bbMultiplier, secondaryThreshold)
}

func qqeModEnhancedWithRSI(rsiValues []float64, rsiOK bool, rsiPeriod int, smoothing int, primaryFactor float64, secondaryFactor float64, bbPeriod int, bbMultiplier float64, secondaryThreshold float64) (qqeModEnhancedResult, bool) {
	smoothed, smoothedDeltas, offset, foundationOK := qqeModTrendFoundation(rsiValues, rsiOK, rsiPeriod, smoothing)
	return qqeModEnhancedFromFoundation(smoothed, smoothedDeltas, offset, primaryFactor, secondaryFactor, bbPeriod, bbMultiplier, secondaryThreshold, foundationOK)
}

func qqeModEnhancedFromFoundation(smoothed []float64, smoothedDeltas []float64, offset int, primaryFactor float64, secondaryFactor float64, bbPeriod int, bbMultiplier float64, secondaryThreshold float64, foundationOK bool) (qqeModEnhancedResult, bool) {
	if !foundationOK {
		return qqeModEnhancedResult{}, false
	}
	primaryTrend, _, okPrimary := qqeModTrendSeriesFromFoundation(smoothed, smoothedDeltas, offset, primaryFactor, false)
	secondaryTrend, _, okSecondary := qqeModTrendSeriesFromFoundation(smoothed, smoothedDeltas, offset, secondaryFactor, false)
	if !okPrimary || !okSecondary || bbPeriod <= 0 || len(primaryTrend) < bbPeriod || len(smoothed) < 2 {
		return qqeModEnhancedResult{}, false
	}
	trendWindow := primaryTrend[len(primaryTrend)-bbPeriod:]
	var basis float64
	for _, value := range trendWindow {
		basis += value - 50
	}
	basis /= float64(bbPeriod)
	var variance float64
	for _, value := range trendWindow {
		diff := (value - 50) - basis
		variance += diff * diff
	}
	deviation := math.Sqrt(variance / float64(bbPeriod))
	lastPrimary := smoothed[len(smoothed)-1]
	previousPrimary := smoothed[len(smoothed)-2]
	lastSecondary := lastPrimary
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
	return qqeModTrendSeriesWithRSI(rsiValues, ok, rsiPeriod, smoothing, factor)
}

func qqeModTrendSeriesWithRSI(rsiValues []float64, rsiOK bool, rsiPeriod int, smoothing int, factor float64) ([]float64, []float64, bool) {
	smoothed, smoothedDeltas, offset, ok := qqeModTrendFoundation(rsiValues, rsiOK, rsiPeriod, smoothing)
	if !ok {
		return nil, nil, false
	}
	return qqeModTrendSeriesFromFoundation(smoothed, smoothedDeltas, offset, factor, true)
}

func qqeModTrendFoundation(rsiValues []float64, rsiOK bool, rsiPeriod int, smoothing int) ([]float64, []float64, int, bool) {
	if !rsiOK || len(rsiValues) < smoothing*3 {
		return nil, nil, 0, false
	}
	smoothed, ok := emaSeries(rsiValues, smoothing)
	if !ok || len(smoothed) < 3 {
		return nil, nil, 0, false
	}
	deltas := make([]float64, 0, len(smoothed)-1)
	for index := 1; index < len(smoothed); index++ {
		deltas = append(deltas, math.Abs(smoothed[index]-smoothed[index-1]))
	}
	wildersPeriod := rsiPeriod*2 - 1
	smoothedDeltas, ok := emaSeries(deltas, wildersPeriod)
	if !ok || len(smoothedDeltas) < 2 {
		return nil, nil, 0, false
	}
	offset := len(smoothed) - len(smoothedDeltas)
	if offset <= 0 {
		return nil, nil, 0, false
	}
	return smoothed, smoothedDeltas, offset, true
}

func qqeModTrendSeriesFromFoundation(smoothed []float64, smoothedDeltas []float64, offset int, factor float64, collectLines bool) ([]float64, []float64, bool) {
	if factor <= 0 || offset <= 0 || len(smoothedDeltas) < 2 || offset+len(smoothedDeltas) > len(smoothed) {
		return nil, nil, false
	}
	longBand := smoothed[offset] - smoothedDeltas[0]*factor
	shortBand := smoothed[offset] + smoothedDeltas[0]*factor
	trendDirection := 1
	trendValues := make([]float64, 0, len(smoothedDeltas))
	var lineValues []float64
	if collectLines {
		lineValues = make([]float64, 0, len(smoothedDeltas))
	}
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
		if collectLines {
			lineValues = append(lineValues, line)
		}
	}
	if len(trendValues) < 2 || (collectLines && len(lineValues) < 2) {
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
	return qqeModWithRSI(rsiValues, ok, rsiPeriod, smoothing, factor)
}

func qqeModWithRSI(rsiValues []float64, rsiOK bool, rsiPeriod int, smoothing int, factor float64) (float64, float64, float64, float64, bool) {
	smoothed, smoothedDeltas, _, foundationOK := qqeModTrendFoundation(rsiValues, rsiOK, rsiPeriod, smoothing)
	return qqeModFromFoundation(smoothed, smoothedDeltas, rsiPeriod, smoothing, factor, foundationOK)
}

func qqeModFromFoundation(smoothed []float64, smoothedDeltas []float64, rsiPeriod int, smoothing int, factor float64, foundationOK bool) (float64, float64, float64, float64, bool) {
	if !foundationOK || factor <= 0 || len(smoothed) < smoothing+2 {
		return 0, 0, 0, 0, false
	}
	wildersPeriod := rsiPeriod*2 - 1
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

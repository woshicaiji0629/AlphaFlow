package indicatorcalc

type aiSupertrendState struct {
	previousPoint    trendPoint
	lastPoint        trendPoint
	ama              float64
	targetFactor     float64
	performanceIndex float64
	bestCentroid     float64
	averageCentroid  float64
	worstCentroid    float64
	cluster          string
}

type aiSupertrendFactorResult struct {
	factor float64
	perf   float64
}

type aiPerformanceCluster struct {
	name     string
	centroid float64
	factors  []float64
	perfs    []float64
}

func addAISupertrendToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, minFactor float64, maxFactor float64, step float64, perfAlpha int) {
	addAISupertrendWithATRToSet(target, values, signals, highs, lows, closes, period, minFactor, maxFactor, step, perfAlpha, nil, false)
}

func addAISupertrendWithATRToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, minFactor float64, maxFactor float64, step float64, perfAlpha int, atrValues []float64, atrOK bool) {
	state, ok := aiSupertrendWithATR(highs, lows, closes, period, minFactor, maxFactor, step, perfAlpha, atrValues, atrOK)
	if !ok {
		return
	}
	lastPoint := state.lastPoint
	lastClose := closes[len(closes)-1]
	setValueTarget(target, values, "ai_supertrend", lastPoint.value, true)
	setValueTarget(target, values, "ai_supertrend_ama", state.ama, true)
	setValueTarget(target, values, "ai_supertrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValueTarget(target, values, "ai_supertrend_target_factor", state.targetFactor, true)
	setValueTarget(target, values, "ai_supertrend_performance_index", state.performanceIndex, true)
	setValueTarget(target, values, "ai_supertrend_best_centroid", state.bestCentroid, true)
	setValueTarget(target, values, "ai_supertrend_average_centroid", state.averageCentroid, true)
	setValueTarget(target, values, "ai_supertrend_worst_centroid", state.worstCentroid, true)
	signals["ai_supertrend_direction"] = lastPoint.direction.String()
	signals["ai_supertrend_flip"] = trendFlip(state.previousPoint.direction.String(), lastPoint.direction.String())
	signals["ai_supertrend_cluster"] = state.cluster
	signals["ai_supertrend_factor_cluster"] = state.cluster
}

func aiSupertrend(highs []float64, lows []float64, closes []float64, period int, minFactor float64, maxFactor float64, step float64, perfAlpha int) (aiSupertrendState, bool) {
	return aiSupertrendWithATR(highs, lows, closes, period, minFactor, maxFactor, step, perfAlpha, nil, false)
}

func aiSupertrendWithATR(highs []float64, lows []float64, closes []float64, period int, minFactor float64, maxFactor float64, step float64, perfAlpha int, atrValues []float64, atrOK bool) (aiSupertrendState, bool) {
	if period <= 0 || minFactor <= 0 || maxFactor < minFactor || step <= 0 || perfAlpha < 2 ||
		len(closes) <= period+2 || len(highs) != len(closes) || len(lows) != len(closes) {
		return aiSupertrendState{}, false
	}
	var ok bool
	if !atrOK {
		atrValues, ok = atrSeries(highs, lows, closes, period)
	} else {
		ok = true
	}
	if !ok || len(atrValues) < 2 {
		return aiSupertrendState{}, false
	}
	offset := len(closes) - len(atrValues)
	results := []aiSupertrendFactorResult{}
	for factor := minFactor; factor <= maxFactor+0.00000001; factor += step {
		result, ok := aiSupertrendFactorPerformance(highs, lows, closes, atrValues, offset, factor, perfAlpha)
		if ok {
			results = append(results, result)
		}
	}
	if len(results) < 3 {
		return aiSupertrendState{}, false
	}
	clusters, ok := aiPerformanceClusters(results)
	if !ok {
		return aiSupertrendState{}, false
	}
	best := clusters[len(clusters)-1]
	if len(best.factors) == 0 || len(best.perfs) == 0 {
		return aiSupertrendState{}, false
	}
	targetFactor := averageFloat(best.factors)
	denominator, ok := aiPerformanceDenominator(closes, perfAlpha)
	if !ok || denominator <= 0 {
		return aiSupertrendState{}, false
	}
	performanceIndex := maxFloat(averageFloat(best.perfs), 0) / denominator
	previousPoint, lastPoint, ama, ok := supertrendATRFactorSummary(highs, lows, closes, atrValues, offset, targetFactor, performanceIndex)
	if !ok {
		return aiSupertrendState{}, false
	}
	return aiSupertrendState{
		previousPoint:    previousPoint,
		lastPoint:        lastPoint,
		ama:              ama,
		targetFactor:     targetFactor,
		performanceIndex: performanceIndex,
		bestCentroid:     clusters[2].centroid,
		averageCentroid:  clusters[1].centroid,
		worstCentroid:    clusters[0].centroid,
		cluster:          best.name,
	}, true
}

func supertrendATRFactorSummary(highs []float64, lows []float64, closes []float64, atrValues []float64, offset int, factor float64, amaFactor float64) (trendPoint, trendPoint, float64, bool) {
	if offset <= 0 || offset >= len(closes)-1 || len(highs) != len(closes) || len(lows) != len(closes) || len(atrValues)+offset != len(closes) || factor <= 0 {
		return trendPoint{}, trendPoint{}, 0, false
	}
	atrValue := atrValues[0] * factor
	if atrValue <= 0 {
		return trendPoint{}, trendPoint{}, 0, false
	}
	mid := (highs[offset] + lows[offset]) / 2
	finalUpper := mid + atrValue
	finalLower := mid - atrValue
	direction := "down"
	if closes[offset] >= mid {
		direction = "up"
	}
	lastPoint := supertrendPoint(finalUpper, finalLower, direction)
	previousPoint := lastPoint
	ama := lastPoint.value
	for index := offset + 1; index < len(closes); index++ {
		atrValue = atrValues[index-offset] * factor
		if atrValue <= 0 {
			return trendPoint{}, trendPoint{}, 0, false
		}
		mid = (highs[index] + lows[index]) / 2
		basicUpper := mid + atrValue
		basicLower := mid - atrValue
		nextUpper := finalUpper
		if basicUpper < finalUpper || closes[index-1] > finalUpper {
			nextUpper = basicUpper
		}
		nextLower := finalLower
		if basicLower > finalLower || closes[index-1] < finalLower {
			nextLower = basicLower
		}
		if direction == "down" && closes[index] > nextUpper {
			direction = "up"
		} else if direction == "up" && closes[index] < nextLower {
			direction = "down"
		}
		finalUpper, finalLower = nextUpper, nextLower
		previousPoint = lastPoint
		lastPoint = supertrendPoint(finalUpper, finalLower, direction)
		ama += amaFactor * (lastPoint.value - ama)
	}
	return previousPoint, lastPoint, ama, true
}

func aiSupertrendFactorPerformance(highs []float64, lows []float64, closes []float64, atrValues []float64, offset int, factor float64, perfAlpha int) (aiSupertrendFactorResult, bool) {
	if offset <= 0 || offset >= len(closes) || len(atrValues)+offset != len(closes) {
		return aiSupertrendFactorResult{}, false
	}
	upper := (highs[offset] + lows[offset]) / 2
	lower := upper
	output := upper
	trend := "down"
	performance := 0.0
	alpha := 2 / float64(perfAlpha+1)
	for index := offset; index < len(closes); index++ {
		atrValue := atrValues[index-offset]
		if atrValue <= 0 {
			return aiSupertrendFactorResult{}, false
		}
		mid := (highs[index] + lows[index]) / 2
		up := mid + atrValue*factor
		down := mid - atrValue*factor
		previousUpper := upper
		previousLower := lower
		previousOutput := output
		if closes[index] > previousUpper {
			trend = "up"
		} else if closes[index] < previousLower {
			trend = "down"
		}
		if index > 0 && closes[index-1] < previousUpper {
			upper = minFloat(up, previousUpper)
		} else {
			upper = up
		}
		if index > 0 && closes[index-1] > previousLower {
			lower = maxFloat(down, previousLower)
		} else {
			lower = down
		}
		if index > offset {
			priceDirection := signFloat(closes[index-1] - previousOutput)
			performance += alpha * ((closes[index]-closes[index-1])*priceDirection - performance)
		}
		if trend == "up" {
			output = lower
		} else {
			output = upper
		}
	}
	return aiSupertrendFactorResult{factor: factor, perf: performance}, true
}

func aiPerformanceDenominator(closes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) < period+1 {
		return 0, false
	}
	seed := 0.0
	for index := 1; index <= period; index++ {
		seed += absFloat(closes[index] - closes[index-1])
	}
	current := seed / float64(period)
	multiplier := 2 / float64(period+1)
	for index := period + 1; index < len(closes); index++ {
		change := absFloat(closes[index] - closes[index-1])
		current = (change-current)*multiplier + current
	}
	return current, true
}

func aiSupertrendAMA(points []trendPoint, performanceIndex float64) float64 {
	if len(points) == 0 {
		return 0
	}
	ama := points[0].value
	for _, point := range points[1:] {
		ama += performanceIndex * (point.value - ama)
	}
	return ama
}

func averageFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func signFloat(value float64) float64 {
	switch {
	case value > 0:
		return 1
	case value < 0:
		return -1
	default:
		return 0
	}
}

func supertrendSeriesWithATRFactor(highs []float64, lows []float64, closes []float64, atrValues []float64, offset int, factor float64) ([]trendPoint, bool) {
	assignedATR := make([]float64, len(closes))
	for atrIndex, value := range atrValues {
		assignedATR[atrIndex+offset] = value * factor
	}
	return supertrendSeriesWithATR(highs, lows, closes, assignedATR, offset, 1)
}

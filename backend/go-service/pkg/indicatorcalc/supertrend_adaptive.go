package indicatorcalc

type adaptiveSupertrendState struct {
	points       []trendPoint
	assignedATR  float64
	highCentroid float64
	midCentroid  float64
	lowCentroid  float64
	cluster      string
}

type volatilityCluster struct {
	assignedATR  float64
	highCentroid float64
	midCentroid  float64
	lowCentroid  float64
	cluster      string
}

func addAdaptiveSupertrendWithState(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64, trainingPeriod int, basic *basicIndicatorState) {
	addAdaptiveSupertrendWithStateToSet(nil, values, signals, highs, lows, closes, period, multiplier, trainingPeriod, basic)
}

func addAdaptiveSupertrendWithStateToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64, trainingPeriod int, basic *basicIndicatorState) {
	state, ok := adaptiveSupertrendState{}, false
	if basic != nil && period == 10 && multiplier == 3 && trainingPeriod == 100 {
		state, ok = basic.adaptiveSupertrendValue()
	}
	if !ok {
		state, ok = adaptiveSupertrend(highs, lows, closes, period, multiplier, trainingPeriod)
	}
	if !ok {
		return
	}
	lastIndex := len(state.points) - 1
	lastPoint := state.points[lastIndex]
	lastClose := closes[len(closes)-1]
	setValueTarget(target, values, "adaptive_supertrend", lastPoint.value, true)
	setValueTarget(target, values, "adaptive_supertrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValueTarget(target, values, "adaptive_supertrend_assigned_atr", state.assignedATR, true)
	setValueTarget(target, values, "adaptive_supertrend_high_centroid", state.highCentroid, true)
	setValueTarget(target, values, "adaptive_supertrend_mid_centroid", state.midCentroid, true)
	setValueTarget(target, values, "adaptive_supertrend_low_centroid", state.lowCentroid, true)
	signals["adaptive_supertrend_direction"] = lastPoint.direction.String()
	signals["adaptive_supertrend_flip"] = trendFlip(state.points[lastIndex-1].direction.String(), lastPoint.direction.String())
	signals["adaptive_supertrend_volatility_cluster"] = state.cluster
}

func adaptiveSupertrend(highs []float64, lows []float64, closes []float64, period int, multiplier float64, trainingPeriod int) (adaptiveSupertrendState, bool) {
	if period <= 0 || trainingPeriod <= 1 || len(closes) <= period+trainingPeriod ||
		len(highs) != len(closes) || len(lows) != len(closes) {
		return adaptiveSupertrendState{}, false
	}
	atrValues, ok := atrSeries(highs, lows, closes, period)
	if !ok || len(atrValues) < trainingPeriod {
		return adaptiveSupertrendState{}, false
	}
	offset := len(closes) - len(atrValues)
	assignedATR := make([]float64, len(closes))
	var lastCluster volatilityCluster
	hasCluster := false
	for atrIndex := trainingPeriod - 1; atrIndex < len(atrValues); atrIndex++ {
		cluster, ok := adaptiveVolatilityCluster(atrValues[atrIndex-trainingPeriod+1:atrIndex+1], atrValues[atrIndex])
		if !ok {
			continue
		}
		assignedATR[atrIndex+offset] = cluster.assignedATR
		lastCluster = cluster
		hasCluster = true
	}
	if !hasCluster {
		return adaptiveSupertrendState{}, false
	}
	points, ok := supertrendSeriesWithATR(highs, lows, closes, assignedATR, offset+trainingPeriod-1, multiplier)
	if !ok {
		return adaptiveSupertrendState{}, false
	}
	return adaptiveSupertrendState{
		points:       points,
		assignedATR:  lastCluster.assignedATR,
		highCentroid: lastCluster.highCentroid,
		midCentroid:  lastCluster.midCentroid,
		lowCentroid:  lastCluster.lowCentroid,
		cluster:      lastCluster.cluster,
	}, true
}

func adaptiveVolatilityCluster(values []float64, current float64) (volatilityCluster, bool) {
	if len(values) == 0 {
		return volatilityCluster{}, false
	}
	high := highestValue(values)
	low := lowestValue(values)
	highMean := low + (high-low)*0.75
	midMean := low + (high-low)*0.5
	lowMean := low + (high-low)*0.25
	for iteration := 0; iteration < 20; iteration++ {
		var highSum, midSum, lowSum float64
		var highCount, midCount, lowCount int
		for _, value := range values {
			highDistance := absFloat(value - highMean)
			midDistance := absFloat(value - midMean)
			lowDistance := absFloat(value - lowMean)
			switch {
			case highDistance <= midDistance && highDistance <= lowDistance:
				highSum += value
				highCount++
			case midDistance <= highDistance && midDistance <= lowDistance:
				midSum += value
				midCount++
			default:
				lowSum += value
				lowCount++
			}
		}
		nextHigh := highMean
		nextMid := midMean
		nextLow := lowMean
		if highCount > 0 {
			nextHigh = highSum / float64(highCount)
		}
		if midCount > 0 {
			nextMid = midSum / float64(midCount)
		}
		if lowCount > 0 {
			nextLow = lowSum / float64(lowCount)
		}
		if absFloat(nextHigh-highMean) < 0.00000001 &&
			absFloat(nextMid-midMean) < 0.00000001 &&
			absFloat(nextLow-lowMean) < 0.00000001 {
			highMean, midMean, lowMean = nextHigh, nextMid, nextLow
			break
		}
		highMean, midMean, lowMean = nextHigh, nextMid, nextLow
	}
	cluster := volatilityCluster{
		highCentroid: highMean,
		midCentroid:  midMean,
		lowCentroid:  lowMean,
	}
	highDistance := absFloat(current - highMean)
	midDistance := absFloat(current - midMean)
	lowDistance := absFloat(current - lowMean)
	switch {
	case highDistance <= midDistance && highDistance <= lowDistance:
		cluster.assignedATR = highMean
		cluster.cluster = "high"
	case midDistance <= highDistance && midDistance <= lowDistance:
		cluster.assignedATR = midMean
		cluster.cluster = "medium"
	default:
		cluster.assignedATR = lowMean
		cluster.cluster = "low"
	}
	return cluster, true
}

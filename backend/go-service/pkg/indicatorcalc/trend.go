package indicatorcalc

func addTrendFeatures(values map[string]string, signals map[string]string, closes []float64) {
	ema7, ok7 := ema(closes, 7)
	ema25, ok25 := ema(closes, 25)
	ema99, ok99 := ema(closes, 99)
	last := closes[len(closes)-1]
	if ok7 && ok25 && ok99 {
		setValue(values, "price_ema7_distance_pct", percentDistance(last, ema7), true)
		setValue(values, "price_ema25_distance_pct", percentDistance(last, ema25), true)
		setValue(values, "price_ema99_distance_pct", percentDistance(last, ema99), true)
		switch {
		case ema7 > ema25 && ema25 > ema99:
			signals["ema_alignment"] = "bull"
		case ema7 < ema25 && ema25 < ema99:
			signals["ema_alignment"] = "bear"
		default:
			signals["ema_alignment"] = "mixed"
		}
	}
	if len(closes) >= 35 {
		recent, okRecent := ema(closes, 25)
		prev, okPrev := ema(closes[:len(closes)-5], 25)
		if okRecent && okPrev && prev != 0 {
			slope := (recent - prev) / prev * 100
			setValue(values, "ema25_slope5_pct", slope, true)
			switch {
			case slope > 0.15:
				signals["trend_direction"] = "up"
			case slope < -0.15:
				signals["trend_direction"] = "down"
			default:
				signals["trend_direction"] = "range"
			}
		}
	}
}

func addSupertrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64) {
	points, ok := supertrendSeries(highs, lows, closes, period, multiplier)
	if !ok {
		return
	}
	lastIndex := len(points) - 1
	lastPoint := points[lastIndex]
	lastClose := closes[len(closes)-1]
	setValue(values, "supertrend", lastPoint.value, true)
	setValue(values, "supertrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValue(values, "supertrend_stop_distance_pct", absFloat(percentDistance(lastClose, lastPoint.value)), lastPoint.value != 0)
	signals["supertrend_direction"] = lastPoint.direction
	signals["supertrend_flip"] = trendFlip(points[lastIndex-1].direction, lastPoint.direction)

	for _, preset := range []struct {
		name       string
		period     int
		multiplier float64
	}{
		{name: "supertrend_7_2", period: 7, multiplier: 2},
		{name: "supertrend_10_3", period: 10, multiplier: 3},
		{name: "supertrend_10_3_3", period: 10, multiplier: 3.3},
		{name: "supertrend_14_4", period: 14, multiplier: 4},
	} {
		presetValue, presetDirection, presetOK := supertrend(highs, lows, closes, preset.period, preset.multiplier)
		if !presetOK {
			continue
		}
		setValue(values, preset.name, presetValue, true)
		signals[preset.name+"_direction"] = presetDirection
	}

	addAdaptiveSupertrend(values, signals, highs, lows, closes, 10, 3, 100)
	addAISupertrend(values, signals, highs, lows, closes, 10, 1, 5, 0.5, 10)

	zone, zoneOK := supertrendZone(highs, lows, closes, points, period, 14, 1.5)
	if !zoneOK {
		signals["supertrend_zone_ready"] = "false"
		return
	}
	setValue(values, "supertrend_zone_pivot_high", zone.pivotHigh, true)
	setValue(values, "supertrend_zone_pivot_low", zone.pivotLow, true)
	setValue(values, "supertrend_zone_mid", zone.mid, true)
	setValue(values, "supertrend_zone_fib_236", zone.fib236, true)
	setValue(values, "supertrend_zone_fib_382", zone.fib382, true)
	setValue(values, "supertrend_zone_fib_5", zone.fib5, true)
	setValue(values, "supertrend_zone_fib_618", zone.fib618, true)
	setValue(values, "supertrend_zone_fib_786", zone.fib786, true)
	setValue(values, "supertrend_zone_extension_1618", zone.extension, true)
	setValue(values, "supertrend_zone_premium_band", zone.premiumBand, true)
	setValue(values, "supertrend_zone_discount_band", zone.discountBand, true)
	setValue(values, "supertrend_zone_position_pct", zone.positionPct, true)
	signals["supertrend_zone_side"] = zone.side
	signals["supertrend_zone_area"] = zone.area
	signals["supertrend_zone_ready"] = "true"
}

type supertrendZoneState struct {
	pivotHigh    float64
	pivotLow     float64
	mid          float64
	fib236       float64
	fib382       float64
	fib5         float64
	fib618       float64
	fib786       float64
	extension    float64
	premiumBand  float64
	discountBand float64
	positionPct  float64
	side         string
	area         string
}

func supertrendZone(highs []float64, lows []float64, closes []float64, points []trendPoint, supertrendPeriod int, atrPeriod int, atrMultiplier float64) (supertrendZoneState, bool) {
	if len(points) < 2 || len(highs) != len(closes) || len(lows) != len(closes) || supertrendPeriod <= 0 {
		return supertrendZoneState{}, false
	}
	offset := len(closes) - len(points)
	if offset < 0 || offset >= len(closes) {
		return supertrendZoneState{}, false
	}
	var pivotHigh float64
	var pivotLow float64
	hasPivotHigh := false
	hasPivotLow := false
	segmentStart := offset
	previousDirection := points[0].direction
	for pointIndex := 1; pointIndex < len(points); pointIndex++ {
		currentDirection := points[pointIndex].direction
		if currentDirection == previousDirection {
			continue
		}
		seriesIndex := pointIndex + offset
		highest, lowest := highLow(highs[segmentStart:seriesIndex+1], lows[segmentStart:seriesIndex+1])
		if previousDirection == "up" && currentDirection == "down" {
			pivotHigh = highest
			hasPivotHigh = true
		}
		if previousDirection == "down" && currentDirection == "up" {
			pivotLow = lowest
			hasPivotLow = true
		}
		segmentStart = seriesIndex
		previousDirection = currentDirection
	}
	if !hasPivotHigh || !hasPivotLow || pivotHigh == pivotLow {
		return supertrendZoneState{}, false
	}
	if pivotHigh < pivotLow {
		pivotHigh, pivotLow = pivotLow, pivotHigh
	}
	atrValue, ok := atr(highs, lows, closes, atrPeriod)
	if !ok {
		return supertrendZoneState{}, false
	}
	lastPoint := points[len(points)-1]
	lastClose := closes[len(closes)-1]
	priceRange := pivotHigh - pivotLow
	positionPct := (lastClose - pivotLow) / priceRange * 100
	zone := supertrendZoneState{
		pivotHigh:    pivotHigh,
		pivotLow:     pivotLow,
		mid:          pivotLow + priceRange*0.5,
		fib236:       pivotLow + priceRange*0.236,
		fib382:       pivotLow + priceRange*0.382,
		fib5:         pivotLow + priceRange*0.5,
		fib618:       pivotLow + priceRange*0.618,
		fib786:       pivotLow + priceRange*0.786,
		premiumBand:  lastPoint.value + atrValue*atrMultiplier,
		discountBand: lastPoint.value - atrValue*atrMultiplier,
		positionPct:  positionPct,
		side:         supertrendZoneSide(lastPoint.direction),
		area:         supertrendZoneArea(positionPct),
	}
	if lastPoint.direction == "down" {
		zone.extension = pivotLow - priceRange*0.618
	} else {
		zone.extension = pivotLow + priceRange*1.618
	}
	return zone, true
}

func supertrendZoneSide(direction string) string {
	if direction == "down" {
		return "bear"
	}
	return "bull"
}

func supertrendZoneArea(positionPct float64) string {
	switch {
	case positionPct < 0 || positionPct > 100:
		return "extension"
	case positionPct < 38.2:
		return "discount"
	case positionPct <= 61.8:
		return "mid"
	default:
		return "premium"
	}
}

func addAlphaTrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) {
	points, lastMFI, ok := alphaTrendSeries(highs, lows, closes, volumes, period, multiplier)
	if !ok {
		return
	}
	lastIndex := len(points) - 1
	lastPoint := points[lastIndex]
	prevPoint := points[lastIndex-1]
	lastClose := closes[len(closes)-1]
	setValue(values, "alphatrend", lastPoint.value, true)
	setValue(values, "mfi14", lastMFI, true)
	setValue(values, "alphatrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValue(values, "alphatrend_slope_pct", percentDistance(lastPoint.value, prevPoint.value), prevPoint.value != 0)
	signals["alphatrend_direction"] = lastPoint.direction
	signals["alphatrend_flip"] = trendFlip(prevPoint.direction, lastPoint.direction)
	cross, signal := alphaTrendSignals(points)
	signals["alphatrend_cross"] = cross
	signals["alphatrend_signal"] = signal
}

func addPSARFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	value, direction, ok := psar(highs, lows, closes, 0.02, 0.2)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValue(values, "psar", value, true)
	setValue(values, "psar_distance_pct", percentDistance(last, value), value != 0)
	signals["psar_direction"] = direction
}

func addChandelierExit(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64) {
	longStop, shortStop, ok := chandelierExit(highs, lows, closes, period, multiplier)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValue(values, "chandelier_long", longStop, true)
	setValue(values, "chandelier_short", shortStop, true)
	switch {
	case last >= longStop:
		signals["chandelier_direction"] = "up"
		setValue(values, "chandelier_stop_distance_pct", absFloat(percentDistance(last, longStop)), longStop != 0)
	case last <= shortStop:
		signals["chandelier_direction"] = "down"
		setValue(values, "chandelier_stop_distance_pct", absFloat(percentDistance(last, shortStop)), shortStop != 0)
	default:
		signals["chandelier_direction"] = "neutral"
	}
}

func chandelierExit(highs []float64, lows []float64, closes []float64, period int, multiplier float64) (float64, float64, bool) {
	if period <= 0 || len(closes) < period || multiplier <= 0 {
		return 0, 0, false
	}
	atrValue, ok := atr(highs, lows, closes, period)
	if !ok {
		return 0, 0, false
	}
	highest, lowest := highLow(highs[len(highs)-period:], lows[len(lows)-period:])
	return highest - multiplier*atrValue, lowest + multiplier*atrValue, true
}

func supertrend(highs []float64, lows []float64, closes []float64, period int, multiplier float64) (float64, string, bool) {
	points, ok := supertrendSeries(highs, lows, closes, period, multiplier)
	if !ok {
		return 0, "", false
	}
	last := points[len(points)-1]
	return last.value, last.direction, true
}

type trendPoint struct {
	value     float64
	direction string
}

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

type aiSupertrendState struct {
	points           []trendPoint
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

func addAdaptiveSupertrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64, trainingPeriod int) {
	state, ok := adaptiveSupertrend(highs, lows, closes, period, multiplier, trainingPeriod)
	if !ok {
		return
	}
	lastIndex := len(state.points) - 1
	lastPoint := state.points[lastIndex]
	lastClose := closes[len(closes)-1]
	setValue(values, "adaptive_supertrend", lastPoint.value, true)
	setValue(values, "adaptive_supertrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValue(values, "adaptive_supertrend_assigned_atr", state.assignedATR, true)
	setValue(values, "adaptive_supertrend_high_centroid", state.highCentroid, true)
	setValue(values, "adaptive_supertrend_mid_centroid", state.midCentroid, true)
	setValue(values, "adaptive_supertrend_low_centroid", state.lowCentroid, true)
	signals["adaptive_supertrend_direction"] = lastPoint.direction
	signals["adaptive_supertrend_flip"] = trendFlip(state.points[lastIndex-1].direction, lastPoint.direction)
	signals["adaptive_supertrend_volatility_cluster"] = state.cluster
}

func addAISupertrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, minFactor float64, maxFactor float64, step float64, perfAlpha int) {
	state, ok := aiSupertrend(highs, lows, closes, period, minFactor, maxFactor, step, perfAlpha)
	if !ok {
		return
	}
	lastIndex := len(state.points) - 1
	lastPoint := state.points[lastIndex]
	lastClose := closes[len(closes)-1]
	setValue(values, "ai_supertrend", lastPoint.value, true)
	setValue(values, "ai_supertrend_ama", state.ama, true)
	setValue(values, "ai_supertrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValue(values, "ai_supertrend_target_factor", state.targetFactor, true)
	setValue(values, "ai_supertrend_performance_index", state.performanceIndex, true)
	setValue(values, "ai_supertrend_best_centroid", state.bestCentroid, true)
	setValue(values, "ai_supertrend_average_centroid", state.averageCentroid, true)
	setValue(values, "ai_supertrend_worst_centroid", state.worstCentroid, true)
	signals["ai_supertrend_direction"] = lastPoint.direction
	signals["ai_supertrend_flip"] = trendFlip(state.points[lastIndex-1].direction, lastPoint.direction)
	signals["ai_supertrend_cluster"] = state.cluster
	signals["ai_supertrend_factor_cluster"] = state.cluster
}

func aiSupertrend(highs []float64, lows []float64, closes []float64, period int, minFactor float64, maxFactor float64, step float64, perfAlpha int) (aiSupertrendState, bool) {
	if period <= 0 || minFactor <= 0 || maxFactor < minFactor || step <= 0 || perfAlpha < 2 ||
		len(closes) <= period+2 || len(highs) != len(closes) || len(lows) != len(closes) {
		return aiSupertrendState{}, false
	}
	atrValues, ok := atrSeries(highs, lows, closes, period)
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
	points, ok := supertrendSeriesWithATRFactor(highs, lows, closes, atrValues, offset, targetFactor)
	if !ok {
		return aiSupertrendState{}, false
	}
	denominator, ok := aiPerformanceDenominator(closes, perfAlpha)
	if !ok || denominator <= 0 {
		return aiSupertrendState{}, false
	}
	performanceIndex := maxFloat(averageFloat(best.perfs), 0) / denominator
	ama := aiSupertrendAMA(points, performanceIndex)
	return aiSupertrendState{
		points:           points,
		ama:              ama,
		targetFactor:     targetFactor,
		performanceIndex: performanceIndex,
		bestCentroid:     clusters[2].centroid,
		averageCentroid:  clusters[1].centroid,
		worstCentroid:    clusters[0].centroid,
		cluster:          best.name,
	}, true
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

func aiPerformanceClusters(results []aiSupertrendFactorResult) ([]aiPerformanceCluster, bool) {
	if len(results) < 3 {
		return nil, false
	}
	perfs := make([]float64, 0, len(results))
	for _, result := range results {
		perfs = append(perfs, result.perf)
	}
	centroids := []float64{
		percentileSortedCopy(perfs, 0.25),
		percentileSortedCopy(perfs, 0.50),
		percentileSortedCopy(perfs, 0.75),
	}
	assignments := make([]int, len(results))
	for iteration := 0; iteration < 100; iteration++ {
		sums := []float64{0, 0, 0}
		counts := []int{0, 0, 0}
		for index, result := range results {
			cluster := nearestCentroidIndex(result.perf, centroids)
			assignments[index] = cluster
			sums[cluster] += result.perf
			counts[cluster]++
		}
		changed := false
		for index := range centroids {
			if counts[index] == 0 {
				continue
			}
			next := sums[index] / float64(counts[index])
			if absFloat(next-centroids[index]) > 0.00000001 {
				changed = true
			}
			centroids[index] = next
		}
		if !changed {
			break
		}
	}
	clusters := []aiPerformanceCluster{
		{name: "cluster_0", centroid: centroids[0]},
		{name: "cluster_1", centroid: centroids[1]},
		{name: "cluster_2", centroid: centroids[2]},
	}
	for index, result := range results {
		cluster := assignments[index]
		clusters[cluster].factors = append(clusters[cluster].factors, result.factor)
		clusters[cluster].perfs = append(clusters[cluster].perfs, result.perf)
	}
	sortPerformanceClusters(clusters)
	clusters[0].name = "worst"
	clusters[1].name = "average"
	clusters[2].name = "best"
	return clusters, true
}

func aiPerformanceDenominator(closes []float64, period int) (float64, bool) {
	if len(closes) < period+1 {
		return 0, false
	}
	changes := make([]float64, 0, len(closes)-1)
	for index := 1; index < len(closes); index++ {
		changes = append(changes, absFloat(closes[index]-closes[index-1]))
	}
	return ema(changes, period)
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

func nearestCentroidIndex(value float64, centroids []float64) int {
	index := 0
	distance := absFloat(value - centroids[0])
	for nextIndex := 1; nextIndex < len(centroids); nextIndex++ {
		nextDistance := absFloat(value - centroids[nextIndex])
		if nextDistance < distance {
			index = nextIndex
			distance = nextDistance
		}
	}
	return index
}

func percentileSortedCopy(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	for index := 1; index < len(sorted); index++ {
		value := sorted[index]
		position := index - 1
		for position >= 0 && sorted[position] > value {
			sorted[position+1] = sorted[position]
			position--
		}
		sorted[position+1] = value
	}
	if percentile <= 0 {
		return sorted[0]
	}
	if percentile >= 1 {
		return sorted[len(sorted)-1]
	}
	position := percentile * float64(len(sorted)-1)
	lower := int(position)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[lower]
	}
	weight := position - float64(lower)
	return sorted[lower] + (sorted[upper]-sorted[lower])*weight
}

func sortPerformanceClusters(clusters []aiPerformanceCluster) {
	for index := 1; index < len(clusters); index++ {
		value := clusters[index]
		position := index - 1
		for position >= 0 && clusters[position].centroid > value.centroid {
			clusters[position+1] = clusters[position]
			position--
		}
		clusters[position+1] = value
	}
}

func supertrendSeriesWithATRFactor(highs []float64, lows []float64, closes []float64, atrValues []float64, offset int, factor float64) ([]trendPoint, bool) {
	assignedATR := make([]float64, len(closes))
	for atrIndex, value := range atrValues {
		assignedATR[atrIndex+offset] = value * factor
	}
	return supertrendSeriesWithATR(highs, lows, closes, assignedATR, offset, 1)
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

func supertrendSeriesWithATR(highs []float64, lows []float64, closes []float64, atrValues []float64, start int, multiplier float64) ([]trendPoint, bool) {
	if start <= 0 || start >= len(closes) || len(atrValues) != len(closes) {
		return nil, false
	}
	finalUpper := make([]float64, len(closes))
	finalLower := make([]float64, len(closes))
	direction := make([]string, len(closes))
	for index := start; index < len(closes); index++ {
		if atrValues[index] <= 0 {
			return nil, false
		}
		mid := (highs[index] + lows[index]) / 2
		basicUpper := mid + multiplier*atrValues[index]
		basicLower := mid - multiplier*atrValues[index]
		if index == start {
			finalUpper[index] = basicUpper
			finalLower[index] = basicLower
			if closes[index] >= mid {
				direction[index] = "up"
			} else {
				direction[index] = "down"
			}
			continue
		}
		if basicUpper < finalUpper[index-1] || closes[index-1] > finalUpper[index-1] {
			finalUpper[index] = basicUpper
		} else {
			finalUpper[index] = finalUpper[index-1]
		}
		if basicLower > finalLower[index-1] || closes[index-1] < finalLower[index-1] {
			finalLower[index] = basicLower
		} else {
			finalLower[index] = finalLower[index-1]
		}
		switch {
		case direction[index-1] == "down" && closes[index] > finalUpper[index]:
			direction[index] = "up"
		case direction[index-1] == "up" && closes[index] < finalLower[index]:
			direction[index] = "down"
		default:
			direction[index] = direction[index-1]
		}
	}
	points := make([]trendPoint, 0, len(closes)-start)
	for index := start; index < len(closes); index++ {
		if direction[index] == "down" {
			points = append(points, trendPoint{value: finalUpper[index], direction: "down"})
			continue
		}
		points = append(points, trendPoint{value: finalLower[index], direction: "up"})
	}
	if len(points) < 2 {
		return nil, false
	}
	return points, true
}

func supertrendSeries(highs []float64, lows []float64, closes []float64, period int, multiplier float64) ([]trendPoint, bool) {
	if period <= 0 || len(closes) <= period {
		return nil, false
	}
	trs := trueRanges(highs, lows, closes)
	if len(trs) < period {
		return nil, false
	}
	atrValues := make([]float64, len(closes))
	firstATR, _ := sma(trs[:period], period)
	atrValues[period] = firstATR
	for index := period + 1; index < len(closes); index++ {
		atrValues[index] = (atrValues[index-1]*float64(period-1) + trs[index-1]) / float64(period)
	}

	finalUpper := make([]float64, len(closes))
	finalLower := make([]float64, len(closes))
	direction := make([]string, len(closes))
	for index := period; index < len(closes); index++ {
		mid := (highs[index] + lows[index]) / 2
		basicUpper := mid + multiplier*atrValues[index]
		basicLower := mid - multiplier*atrValues[index]
		if index == period {
			finalUpper[index] = basicUpper
			finalLower[index] = basicLower
			if closes[index] >= mid {
				direction[index] = "up"
			} else {
				direction[index] = "down"
			}
			continue
		}
		if basicUpper < finalUpper[index-1] || closes[index-1] > finalUpper[index-1] {
			finalUpper[index] = basicUpper
		} else {
			finalUpper[index] = finalUpper[index-1]
		}
		if basicLower > finalLower[index-1] || closes[index-1] < finalLower[index-1] {
			finalLower[index] = basicLower
		} else {
			finalLower[index] = finalLower[index-1]
		}
		switch {
		case direction[index-1] == "down" && closes[index] > finalUpper[index]:
			direction[index] = "up"
		case direction[index-1] == "up" && closes[index] < finalLower[index]:
			direction[index] = "down"
		default:
			direction[index] = direction[index-1]
		}
	}
	points := make([]trendPoint, 0, len(closes)-period)
	for index := period; index < len(closes); index++ {
		if direction[index] == "down" {
			points = append(points, trendPoint{value: finalUpper[index], direction: "down"})
			continue
		}
		points = append(points, trendPoint{value: finalLower[index], direction: "up"})
	}
	if len(points) < 2 {
		return nil, false
	}
	return points, true
}

func alphaTrend(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) (float64, float64, string, bool) {
	points, lastMFI, ok := alphaTrendSeries(highs, lows, closes, volumes, period, multiplier)
	if !ok {
		return 0, 0, "", false
	}
	last := points[len(points)-1]
	return last.value, lastMFI, last.direction, true
}

func alphaTrendSeries(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) ([]trendPoint, float64, bool) {
	if period <= 0 || len(closes) <= period || len(volumes) != len(closes) {
		return nil, 0, false
	}
	trs := trueRanges(highs, lows, closes)
	if len(trs) < period {
		return nil, 0, false
	}
	trend := make([]float64, len(closes))
	directions := make([]string, len(closes))
	for index := period; index < len(closes); index++ {
		atrValue, _ := sma(trs[index-period:index], period)
		mfi := moneyFlowIndex(highs[:index+1], lows[:index+1], closes[:index+1], volumes[:index+1], period)
		up := lows[index] - multiplier*atrValue
		down := highs[index] + multiplier*atrValue
		if index == period {
			if mfi >= 50 {
				trend[index] = up
				directions[index] = "up"
			} else {
				trend[index] = down
				directions[index] = "down"
			}
			continue
		}
		if mfi >= 50 {
			if up < trend[index-1] {
				trend[index] = trend[index-1]
			} else {
				trend[index] = up
			}
			directions[index] = "up"
		} else {
			if down > trend[index-1] {
				trend[index] = trend[index-1]
			} else {
				trend[index] = down
			}
			directions[index] = "down"
		}
	}
	lastMFI := moneyFlowIndex(highs, lows, closes, volumes, period)
	points := make([]trendPoint, 0, len(closes)-period)
	for index := period; index < len(closes); index++ {
		points = append(points, trendPoint{value: trend[index], direction: directions[index]})
	}
	if len(points) < 2 {
		return nil, 0, false
	}
	return points, lastMFI, true
}

func alphaTrendSignals(points []trendPoint) (string, string) {
	if len(points) < 4 {
		return "none", "none"
	}
	buys := make([]bool, len(points))
	sells := make([]bool, len(points))
	for index := 3; index < len(points); index++ {
		current := points[index].value
		twoBack := points[index-2].value
		previous := points[index-1].value
		threeBack := points[index-3].value
		buys[index] = current > twoBack && previous <= threeBack
		sells[index] = current < twoBack && previous >= threeBack
	}

	last := len(points) - 1
	cross := "none"
	signal := "none"
	if buys[last] {
		cross = "buy"
		if alphaTrendSignalAllowed(previousCrossDistance(buys, last), crossDistance(sells, last)) {
			signal = "buy"
		}
	}
	if sells[last] {
		cross = "sell"
		if alphaTrendSignalAllowed(previousCrossDistance(sells, last), crossDistance(buys, last)) {
			signal = "sell"
		}
	}
	return cross, signal
}

func alphaTrendSignalAllowed(previousSame int, opposite int) bool {
	return previousSame >= 0 && opposite >= 0 && previousSame > opposite
}

type livermoreState struct {
	trend    int
	a        float64
	b        float64
	c        float64
	d        float64
	e        float64
	f        float64
	g        float64
	h        float64
	hasA     bool
	hasB     bool
	hasC     bool
	hasD     bool
	hasE     bool
	hasF     bool
	hasG     bool
	hasH     bool
	buy      bool
	sell     bool
	keyPoint float64
}

func addLivermoreFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, opens []float64) {
	state, ok := livermoreStructure(highs, lows, closes, opens, 365, 4)
	if !ok {
		return
	}
	if state.hasC {
		setValue(values, "livermore_key_point", state.c, true)
	}
	if state.hasF {
		setValue(values, "livermore_pullback_point", state.f, true)
	}
	if state.hasG {
		setValue(values, "livermore_breakout_line", state.g, true)
	}
	if state.hasH {
		setValue(values, "livermore_previous_key_point", state.h, true)
	}
	if state.keyPoint != 0 {
		setValue(values, "livermore_active_point", state.keyPoint, true)
	}
	switch state.trend {
	case 1:
		signals["livermore_trend"] = "up"
	case 2:
		signals["livermore_trend"] = "down"
	default:
		signals["livermore_trend"] = "range"
	}
	switch {
	case state.buy:
		signals["livermore_signal"] = "buy"
	case state.sell:
		signals["livermore_signal"] = "sell"
	default:
		signals["livermore_signal"] = "none"
	}
}

func livermoreStructure(highs []float64, lows []float64, closes []float64, opens []float64, period int, multiplier float64) (livermoreState, bool) {
	atrValues, ok := atrSeries(highs, lows, closes, period)
	if !ok || len(opens) != len(closes) {
		return livermoreState{}, false
	}
	offset := len(closes) - len(atrValues)
	var state livermoreState
	for atrIndex, atrValue := range atrValues {
		index := atrIndex + offset
		da := atrValue * multiplier
		db := da * 0.5
		previousTrend := state.trend
		if state.trend == 0 {
			if closes[index] > opens[index] {
				state.trend = 1
				state.a = highs[index]
				state.d = state.a - da
			} else {
				state.trend = 2
				state.a = lows[index]
				state.d = state.a + da
			}
			state.hasA = true
			state.hasD = true
			state.keyPoint = state.a
			continue
		}
		state.buy = false
		state.sell = false
		if state.trend == 1 {
			updateLivermoreUp(&state, highs[index], lows[index], da, db)
		} else if state.trend == 2 {
			updateLivermoreDown(&state, highs[index], lows[index], da, db)
		}
		state.buy = state.trend == 1 && previousTrend == 2
		state.sell = state.trend == 2 && previousTrend == 1
		if state.hasA {
			state.keyPoint = state.a
		}
	}
	return state, state.trend != 0
}

func updateLivermoreUp(state *livermoreState, high float64, low float64, da float64, db float64) {
	if high > state.a {
		state.a = high
		state.b = state.a - da
		state.hasA = true
		state.hasB = true
		if !state.hasC || high > state.c {
			state.hasC = false
		}
	} else if state.hasB && low < state.b {
		state.hasB = false
		state.c = state.a
		state.d = low
		state.e = state.d + da
		state.hasC = true
		state.hasD = true
		state.hasE = true
	}
	if !state.hasD || low < state.d {
		state.d = low
		state.e = state.d + da
		state.hasD = true
		state.hasE = true
	} else if state.hasE && high > state.e {
		state.hasE = false
		state.f = state.d
		state.g = state.d - db
		state.hasF = true
		state.hasG = true
		if state.hasH && state.g > state.h {
			state.hasH = false
		}
	}
	if (state.hasG && low < state.g) || (state.hasH && low < state.h) {
		state.trend = 2
		if state.hasC {
			state.h = state.c
			state.hasH = true
		}
		state.a = low
		state.b = state.a + da
		state.hasA = true
		state.hasB = true
		state.hasC = false
		state.hasD = false
		state.hasF = false
		state.hasG = false
	}
}

func updateLivermoreDown(state *livermoreState, high float64, low float64, da float64, db float64) {
	if low < state.a {
		state.a = low
		state.b = state.a + da
		state.hasA = true
		state.hasB = true
		if !state.hasC || low < state.c {
			state.hasC = false
		}
	} else if state.hasB && high > state.b {
		state.hasB = false
		state.c = state.a
		state.d = high
		state.e = state.d - da
		state.hasC = true
		state.hasD = true
		state.hasE = true
	}
	if !state.hasD || high > state.d {
		state.d = high
		state.e = state.d - da
		state.hasD = true
		state.hasE = true
	} else if state.hasE && low < state.e {
		state.hasE = false
		state.f = state.d
		state.g = state.d + db
		state.hasF = true
		state.hasG = true
		if state.hasH && state.g < state.h {
			state.hasH = false
		}
	}
	if (state.hasG && high > state.g) || (state.hasH && high > state.h) {
		state.trend = 1
		if state.hasC {
			state.h = state.c
			state.hasH = true
		}
		state.a = high
		state.b = state.a - da
		state.hasA = true
		state.hasB = true
		state.hasC = false
		state.hasD = false
		state.hasF = false
		state.hasG = false
	}
}

func crossDistance(series []bool, current int) int {
	for index := current; index >= 0; index-- {
		if series[index] {
			return current - index
		}
	}
	return -1
}

func previousCrossDistance(series []bool, current int) int {
	for index := current - 1; index >= 0; index-- {
		if series[index] {
			return current - index - 1
		}
	}
	return -1
}

func psar(highs []float64, lows []float64, closes []float64, step float64, maxStep float64) (float64, string, bool) {
	if len(closes) < 3 || step <= 0 || maxStep < step {
		return 0, "", false
	}
	uptrend := closes[1] >= closes[0]
	sar := lows[0]
	ep := highs[0]
	if !uptrend {
		sar = highs[0]
		ep = lows[0]
	}
	acceleration := step
	for index := 1; index < len(closes); index++ {
		sar = sar + acceleration*(ep-sar)
		if uptrend {
			if index >= 2 {
				sar = minFloat(sar, lows[index-1], lows[index-2])
			}
			if lows[index] < sar {
				uptrend = false
				sar = ep
				ep = lows[index]
				acceleration = step
				continue
			}
			if highs[index] > ep {
				ep = highs[index]
				acceleration = minFloat(acceleration+step, maxStep)
			}
			continue
		}
		if index >= 2 {
			sar = maxFloat(sar, highs[index-1], highs[index-2])
		}
		if highs[index] > sar {
			uptrend = true
			sar = ep
			ep = highs[index]
			acceleration = step
			continue
		}
		if lows[index] < ep {
			ep = lows[index]
			acceleration = minFloat(acceleration+step, maxStep)
		}
	}
	if uptrend {
		return sar, "up", true
	}
	return sar, "down", true
}

func minFloat(first float64, values ...float64) float64 {
	result := first
	for _, value := range values {
		if value < result {
			result = value
		}
	}
	return result
}

func maxFloat(first float64, values ...float64) float64 {
	result := first
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func trendFlip(previous string, current string) string {
	if previous == "" || current == "" || previous == current {
		return "none"
	}
	return current
}

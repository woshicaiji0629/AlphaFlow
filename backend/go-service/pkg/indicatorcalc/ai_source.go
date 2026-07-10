package indicatorcalc

import "math"

const (
	aiSourceOpen = iota
	aiSourceHigh
	aiSourceLow
	aiSourceClose
)

type aiSourceConfig struct {
	memoryDepth     int
	kNeighbors      int
	horizonBars     int
	spacingBars     int
	learnATRFactor  float64
	neuralInfluence float64
	learnRate       float64
	huberDelta      float64
	fisherSpeed     float64
	fisherFloor     float64
	minRows         int
	sourceSmoothLen int
	maLength        int
	stLength        int
	stMultiplier    float64
	stAdaptivity    float64
}

type aiSourceRow struct {
	features [6]float64
	outcome  int
}

type aiSourceScore struct {
	analog float64
	agree  float64
	tight  float64
	count  int
	rank   float64
}

type aiSourceNeuralState struct {
	weights [6]float64
	bias    float64
	mom     [7]float64
	vel     [7]float64
	step    int
}

type aiSourceResult struct {
	ma              float64
	sourceValue     float64
	drive           float64
	scoreOpen       float64
	scoreHigh       float64
	scoreLow        float64
	scoreClose      float64
	supertrend      float64
	supertrendDist  float64
	adaptMultiplier float64
	selected        string
	changed         bool
	direction       string
	flip            string
	ready           bool
}

func defaultAISourceConfig() aiSourceConfig {
	return aiSourceConfig{
		memoryDepth:     40,
		kNeighbors:      9,
		horizonBars:     4,
		spacingBars:     4,
		learnATRFactor:  0.45,
		neuralInfluence: 0.35,
		learnRate:       0.01,
		huberDelta:      0.02,
		fisherSpeed:     0.20,
		fisherFloor:     0.40,
		minRows:         80,
		sourceSmoothLen: 3,
		maLength:        50,
		stLength:        10,
		stMultiplier:    1.7,
		stAdaptivity:    0.80,
	}
}

func addAISourceSwitchingFeatures(values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64) {
	addAISourceSwitchingFeaturesWithContext(values, signals, opens, highs, lows, closes, nil)
}

func addAISourceSwitchingFeaturesWithContext(values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64, features *featureContext) {
	cfg := defaultAISourceConfig()
	var atr14, stATR []float64
	var ok14, okST bool
	if features != nil {
		atr14, ok14 = features.atrSeries(14)
		stATR, okST = features.atrSeries(cfg.stLength)
	}
	result, ok := aiSourceSwitchingWithATR(opens, highs, lows, closes, cfg, atr14, ok14, stATR, okST)
	if !ok {
		signals["ai_source_ready"] = "false"
		return
	}
	setValue(values, "ai_source_ma", result.ma, true)
	setValue(values, "ai_source_value", result.sourceValue, true)
	setValue(values, "ai_source_drive", result.drive, true)
	setValue(values, "ai_source_score_open", result.scoreOpen, true)
	setValue(values, "ai_source_score_high", result.scoreHigh, true)
	setValue(values, "ai_source_score_low", result.scoreLow, true)
	setValue(values, "ai_source_score_close", result.scoreClose, true)
	setValue(values, "ai_source_supertrend", result.supertrend, true)
	setValue(values, "ai_source_supertrend_distance_pct", result.supertrendDist, true)
	setValue(values, "ai_source_supertrend_adapt_mult", result.adaptMultiplier, true)
	signals["ai_source_selected"] = result.selected
	signals["ai_source_changed"] = aiSourceBoolSignal(result.changed)
	signals["ai_source_supertrend_direction"] = result.direction
	signals["ai_source_supertrend_flip"] = result.flip
	signals["ai_source_ready"] = aiSourceBoolSignal(result.ready)
}

func aiSourceSwitching(opens []float64, highs []float64, lows []float64, closes []float64, cfg aiSourceConfig) (aiSourceResult, bool) {
	return aiSourceSwitchingWithATR(opens, highs, lows, closes, cfg, nil, false, nil, false)
}

func aiSourceSwitchingWithATR(opens []float64, highs []float64, lows []float64, closes []float64, cfg aiSourceConfig, atr14 []float64, ok14 bool, stATR []float64, okST bool) (aiSourceResult, bool) {
	if !validAISourceInput(opens, highs, lows, closes, cfg) {
		return aiSourceResult{}, false
	}
	if !ok14 {
		atr14, ok14 = atrSeries(highs, lows, closes, 14)
	}
	if !okST {
		stATR, okST = atrSeries(highs, lows, closes, cfg.stLength)
	}
	if !ok14 || !okST {
		return aiSourceResult{}, false
	}
	atr14Offset := len(closes) - len(atr14)
	stATROffset := len(closes) - len(stATR)
	sources := [][]float64{opens, highs, lows, closes}
	featureCaches := [4]aiSourceFeatureCache{}
	for sourceID := range sources {
		featureCaches[sourceID] = newAISourceFeatureCache(sources[sourceID])
	}
	banks := make([][]aiSourceRow, 4)
	for sourceID := range banks {
		banks[sourceID] = make([]aiSourceRow, 0, cfg.memoryDepth)
	}
	allBank := make([]aiSourceRow, 0, cfg.memoryDepth*4)
	weights := [6]float64{1, 1, 1, 1, 1, 1}
	neural := aiSourceNeuralState{weights: [6]float64{0.01, 0.01, 0.01, 0.01, 0.01, 0.01}}
	smoothedSource := make([]float64, 0, len(closes))
	maValues := make([]float64, 0, len(closes))
	stLines := make([]float64, 0, len(closes))
	stDirections := make([]string, 0, len(closes))
	selectedIDs := make([]int, 0, len(closes))
	scoreHistory := make([][4]float64, 0, len(closes))
	driveHistory := make([]float64, 0, len(closes))
	adaptHistory := make([]float64, 0, len(closes))
	var stLong, stShort float64
	stDirection := "bull"
	sourceEMA := newAISourceEMAState(cfg.sourceSmoothLen)
	maEMA := newAISourceEMAState(cfg.maLength)
	for index := range closes {
		features := [4][6]float64{}
		validFeatures := [4]bool{}
		atrValue := atrValueAt(index, atr14, atr14Offset)
		for sourceID := 0; sourceID < 4; sourceID++ {
			features[sourceID], validFeatures[sourceID] = aiSourceFeaturesFromCache(featureCaches[sourceID], sources[sourceID], highs, lows, index, atrValue)
		}
		if sampleIndex := index - cfg.horizonBars; sampleIndex >= 0 {
			outcome := aiSourceOutcome(closes, sampleIndex, index, atrValueAt(sampleIndex, atr14, atr14Offset), cfg.learnATRFactor)
			for sourceID := 0; sourceID < 4; sourceID++ {
				if outcome != 0 {
					sampleFeatures, ok := aiSourceFeaturesFromCache(featureCaches[sourceID], sources[sourceID], highs, lows, sampleIndex, atrValueAt(sampleIndex, atr14, atr14Offset))
					if ok {
						row := aiSourceRow{features: sampleFeatures, outcome: outcome}
						banks[sourceID] = prependAISourceRow(banks[sourceID], row, cfg.memoryDepth)
						allBank = prependAISourceRow(allBank, row, cfg.memoryDepth*4)
					}
				}
			}
			closeFeatures, ok := aiSourceFeaturesFromCache(featureCaches[aiSourceClose], closes, highs, lows, sampleIndex, atrValueAt(sampleIndex, atr14, atr14Offset))
			if ok && outcome != 0 {
				aiSourceTrainNeural(&neural, closeFeatures, outcome, cfg)
			}
		}
		if len(allBank) >= cfg.minRows {
			rawWeights := aiSourceFisherWeights(allBank, cfg.minRows, cfg.fisherFloor)
			for weightIndex := range weights {
				weights[weightIndex] += cfg.fisherSpeed * (rawWeights[weightIndex] - weights[weightIndex])
			}
		}
		ready := aiSourceBanksReady(banks, 20)
		scores := [4]aiSourceScore{}
		ranks := [4]float64{0.25, 0.25, 0.25, 0.25}
		if ready {
			for sourceID := 0; sourceID < 4; sourceID++ {
				if validFeatures[sourceID] {
					scores[sourceID] = aiSourceKNNScore(features[sourceID], banks[sourceID], weights, cfg)
					ranks[sourceID] = aiSourceRank(features[sourceID], scores[sourceID], neural, cfg)
				}
			}
		}
		selected := aiSourceBestID(ranks)
		hardSource := sources[selected][index]
		sourceValue := sourceEMA.append(hardSource)
		smoothedSource = append(smoothedSource, sourceValue)
		maValue := maEMA.append(sourceValue)
		maValues = append(maValues, maValue)
		avgAnalog := (scores[0].analog + scores[1].analog + scores[2].analog + scores[3].analog) / 4
		avgAgree := (scores[0].agree + scores[1].agree + scores[2].agree + scores[3].agree) / 4
		avgTight := (scores[0].tight + scores[1].tight + scores[2].tight + scores[3].tight) / 4
		drive := clampFloat(absFloat(avgAnalog)*0.20+avgAgree*0.40+avgTight*0.40, 0, 1)
		adaptMult := cfg.stMultiplier * (1 + cfg.stAdaptivity*(1-drive))
		stATRValue := atrValueAt(index, stATR, stATROffset)
		line := sourceValue
		if stATRValue > 0 {
			upBand := sourceValue - adaptMult*stATRValue
			downBand := sourceValue + adaptMult*stATRValue
			if len(stLines) == 0 {
				stLong = upBand
				stShort = downBand
			} else {
				if closes[index-1] > stLong {
					stLong = maxFloat(upBand, stLong)
				} else {
					stLong = upBand
				}
				if closes[index-1] < stShort {
					stShort = minFloat(downBand, stShort)
				} else {
					stShort = downBand
				}
				if stDirection == "bear" && closes[index] > stShort {
					stDirection = "bull"
				} else if stDirection == "bull" && closes[index] < stLong {
					stDirection = "bear"
				}
			}
			if stDirection == "bull" {
				line = stLong
			} else {
				line = stShort
			}
		}
		stLines = append(stLines, line)
		stDirections = append(stDirections, stDirection)
		selectedIDs = append(selectedIDs, selected)
		scoreHistory = append(scoreHistory, [4]float64{ranks[0], ranks[1], ranks[2], ranks[3]})
		driveHistory = append(driveHistory, drive)
		adaptHistory = append(adaptHistory, adaptMult)
	}
	last := len(closes) - 1
	if len(maValues) == 0 || len(stLines) < 2 {
		return aiSourceResult{}, false
	}
	flip := "none"
	if stDirections[last-1] == "bear" && stDirections[last] == "bull" {
		flip = "buy"
	} else if stDirections[last-1] == "bull" && stDirections[last] == "bear" {
		flip = "sell"
	}
	return aiSourceResult{
		ma:              maValues[last],
		sourceValue:     smoothedSource[last],
		drive:           driveHistory[last],
		scoreOpen:       scoreHistory[last][0],
		scoreHigh:       scoreHistory[last][1],
		scoreLow:        scoreHistory[last][2],
		scoreClose:      scoreHistory[last][3],
		supertrend:      stLines[last],
		supertrendDist:  percentDistance(closes[last], stLines[last]),
		adaptMultiplier: adaptHistory[last],
		selected:        aiSourceName(selectedIDs[last]),
		changed:         len(selectedIDs) >= 2 && selectedIDs[last] != selectedIDs[last-1],
		direction:       stDirections[last],
		flip:            flip,
		ready:           aiSourceBanksReady(banks, 20),
	}, true
}

func validAISourceInput(opens []float64, highs []float64, lows []float64, closes []float64, cfg aiSourceConfig) bool {
	minLength := 140
	if required := cfg.horizonBars + cfg.maLength + cfg.memoryDepth; required > minLength {
		minLength = required
	}
	return len(closes) >= minLength && len(opens) == len(closes) && len(highs) == len(closes) && len(lows) == len(closes)
}

type aiSourceFeatureCache struct {
	points []aiSourceFeatureCachePoint
}

type aiSourceFeatureCachePoint struct {
	ema10      float64
	ema34      float64
	sma30      float64
	stddev30   float64
	stddev20   float64
	volLow100  float64
	volHigh100 float64
}

func newAISourceFeatureCache(source []float64) aiSourceFeatureCache {
	cache := aiSourceFeatureCache{
		points: make([]aiSourceFeatureCachePoint, len(source)),
	}
	ema10 := newAISourceEMAState(10)
	ema34 := newAISourceEMAState(34)
	sum30 := 0.0
	sumSq30 := 0.0
	sum20 := 0.0
	sumSq20 := 0.0
	volLowWindow := newFloatMonotonicWindow(false)
	volHighWindow := newFloatMonotonicWindow(true)
	for index, value := range source {
		point := &cache.points[index]
		point.ema10 = ema10.append(value)
		point.ema34 = ema34.append(value)
		sum30 += value
		sumSq30 += value * value
		if index >= 30 {
			drop := source[index-30]
			sum30 -= drop
			sumSq30 -= drop * drop
		}
		if index >= 29 {
			mean := sum30 / 30
			point.sma30 = mean
			point.stddev30 = math.Sqrt(math.Max(sumSq30/30-mean*mean, 0))
		}
		sum20 += value
		sumSq20 += value * value
		if index >= 20 {
			drop := source[index-20]
			sum20 -= drop
			sumSq20 -= drop * drop
		}
		if index >= 19 {
			mean := sum20 / 20
			point.stddev20 = math.Sqrt(math.Max(sumSq20/20-mean*mean, 0))
			volLowWindow.push(index, point.stddev20)
			volHighWindow.push(index, point.stddev20)
			volLowWindow.expireBefore(index - 99)
			volHighWindow.expireBefore(index - 99)
			point.volLow100, _ = volLowWindow.value()
			point.volHigh100, _ = volHighWindow.value()
		}
	}
	return cache
}

func aiSourceFeaturesFromCache(cache aiSourceFeatureCache, source []float64, highs []float64, lows []float64, index int, atrValue float64) ([6]float64, bool) {
	var result [6]float64
	if index < 100 || index >= len(source) || atrValue <= 0 {
		return result, false
	}
	point := cache.points[index]
	fast := point.ema10
	slow := point.ema34
	mean := point.sma30
	dev := point.stddev30
	if dev == 0 || source[index-14] == 0 {
		return result, false
	}
	volNow := point.stddev20
	volLow := point.volLow100
	volHigh := point.volHigh100
	priceRange := highs[index] - lows[index]
	if priceRange == 0 {
		priceRange = 1
	}
	result[0] = clampFloat((fast-slow)/atrValue, -3, 3) / 3
	result[1] = clampFloat(-(source[index]-mean)/dev, -3, 3) / 3
	result[2] = clampFloat(((source[index]/source[index-14])-1)/0.05, -3, 3) / 3
	result[3] = scaleValue01(volNow, volLow, volHigh)*2 - 1
	result[4] = clampFloat(((source[index]-lows[index])/priceRange)*2-1, -1, 1)
	result[5] = clampFloat((source[index]-source[index-3])/atrValue, -3, 3) / 3
	return result, true
}

func aiSourceFeatures(source []float64, highs []float64, lows []float64, index int, atrValue float64) ([6]float64, bool) {
	var result [6]float64
	if index < 100 || index >= len(source) || atrValue <= 0 {
		return result, false
	}
	fast, okFast := ema(source[:index+1], 10)
	slow, okSlow := ema(source[:index+1], 34)
	mean, okMean := sma(source[:index+1], 30)
	dev, okDev := standardDeviation(source[:index+1], 30)
	if !okFast || !okSlow || !okMean || !okDev || dev == 0 || source[index-14] == 0 {
		return result, false
	}
	volNow, okVol := standardDeviation(source[:index+1], 20)
	if !okVol {
		return result, false
	}
	volLow := volNow
	volHigh := volNow
	for lookback := index - 99; lookback <= index; lookback++ {
		vol, ok := standardDeviation(source[:lookback+1], 20)
		if !ok {
			continue
		}
		volLow = math.Min(volLow, vol)
		volHigh = math.Max(volHigh, vol)
	}
	priceRange := highs[index] - lows[index]
	if priceRange == 0 {
		priceRange = 1
	}
	result[0] = clampFloat((fast-slow)/atrValue, -3, 3) / 3
	result[1] = clampFloat(-(source[index]-mean)/dev, -3, 3) / 3
	result[2] = clampFloat(((source[index]/source[index-14])-1)/0.05, -3, 3) / 3
	result[3] = scaleValue01(volNow, volLow, volHigh)*2 - 1
	result[4] = clampFloat(((source[index]-lows[index])/priceRange)*2-1, -1, 1)
	result[5] = clampFloat((source[index]-source[index-3])/atrValue, -3, 3) / 3
	return result, true
}

func aiSourceOutcome(closes []float64, sampleIndex int, currentIndex int, atrValue float64, factor float64) int {
	if sampleIndex < 0 || currentIndex >= len(closes) || atrValue <= 0 {
		return 0
	}
	move := closes[currentIndex] - closes[sampleIndex]
	band := factor * atrValue
	switch {
	case move > 2*band:
		return 3
	case move > band:
		return 2
	case move > 0:
		return 1
	case move < -2*band:
		return -3
	case move < -band:
		return -2
	case move < 0:
		return -1
	default:
		return 0
	}
}

func prependAISourceRow(rows []aiSourceRow, row aiSourceRow, limit int) []aiSourceRow {
	if limit <= 0 {
		return rows[:0]
	}
	if len(rows) < limit {
		rows = append(rows, aiSourceRow{})
	}
	copy(rows[1:], rows[:len(rows)-1])
	rows[0] = row
	return rows
}

func aiSourceBanksReady(banks [][]aiSourceRow, minimum int) bool {
	for _, bank := range banks {
		if len(bank) <= minimum {
			return false
		}
	}
	return true
}

func aiSourceFisherWeights(rows []aiSourceRow, minRows int, floor float64) [6]float64 {
	weights := [6]float64{1, 1, 1, 1, 1, 1}
	if len(rows) < minRows {
		return weights
	}
	var sumBull, sumBear, squareBull, squareBear [6]float64
	bullCount := 0
	bearCount := 0
	for _, row := range rows {
		if row.outcome == 0 {
			continue
		}
		isBull := row.outcome > 0
		if isBull {
			bullCount++
		} else {
			bearCount++
		}
		for index, value := range row.features {
			if isBull {
				sumBull[index] += value
				squareBull[index] += value * value
			} else {
				sumBear[index] += value
				squareBear[index] += value * value
			}
		}
	}
	if bullCount <= 3 || bearCount <= 3 {
		return weights
	}
	fisher := [6]float64{}
	maxFisher := 0.0
	for index := range fisher {
		meanBull := sumBull[index] / float64(bullCount)
		meanBear := sumBear[index] / float64(bearCount)
		varBull := math.Max(0, squareBull[index]/float64(bullCount)-meanBull*meanBull)
		varBear := math.Max(0, squareBear[index]/float64(bearCount)-meanBear*meanBear)
		fisher[index] = (meanBull - meanBear) * (meanBull - meanBear) / (varBull + varBear + 0.000001)
		maxFisher = math.Max(maxFisher, fisher[index])
	}
	for index := range weights {
		if maxFisher > 0 {
			weights[index] = math.Max(floor, fisher[index]/maxFisher*8)
		}
	}
	return weights
}

func aiSourceKNNScore(features [6]float64, bank []aiSourceRow, weights [6]float64, cfg aiSourceConfig) aiSourceScore {
	if cfg.kNeighbors <= 0 {
		return aiSourceScore{}
	}
	if cfg.kNeighbors > 16 {
		return aiSourceKNNScoreBatch(features, bank, weights, cfg)
	}
	var gaps [16]float64
	var classes [16]int
	count := 0
	for index, row := range bank {
		if index >= cfg.memoryDepth {
			break
		}
		if index%cfg.spacingBars != 0 || row.outcome == 0 {
			continue
		}
		gap := aiSourceGap(features, row.features, weights)
		if count < cfg.kNeighbors {
			gaps[count] = gap
			classes[count] = row.outcome
			count++
			continue
		}
		worst := 0
		for gapIndex := 0; gapIndex < count; gapIndex++ {
			if gaps[gapIndex] > gaps[worst] {
				worst = gapIndex
			}
		}
		if gap < gaps[worst] {
			gaps[worst] = gap
			classes[worst] = row.outcome
		}
	}
	return aiSourceKNNScoreFromNeighbors(gaps[:], classes[:], count, weights)
}

func aiSourceKNNScoreBatch(features [6]float64, bank []aiSourceRow, weights [6]float64, cfg aiSourceConfig) aiSourceScore {
	gaps := []float64{}
	classes := []int{}
	for index, row := range bank {
		if index >= cfg.memoryDepth {
			break
		}
		if index%cfg.spacingBars != 0 || row.outcome == 0 {
			continue
		}
		gap := aiSourceGap(features, row.features, weights)
		if len(gaps) < cfg.kNeighbors {
			gaps = append(gaps, gap)
			classes = append(classes, row.outcome)
			continue
		}
		worst := 0
		for gapIndex := range gaps {
			if gaps[gapIndex] > gaps[worst] {
				worst = gapIndex
			}
		}
		if gap < gaps[worst] {
			gaps[worst] = gap
			classes[worst] = row.outcome
		}
	}
	return aiSourceKNNScoreFromNeighbors(gaps, classes, len(gaps), weights)
}

func aiSourceKNNScoreFromNeighbors(gaps []float64, classes []int, count int, weights [6]float64) aiSourceScore {
	score := aiSourceScore{count: count}
	total := 0.0
	bull := 0.0
	bear := 0.0
	gapSum := 0.0
	for index := 0; index < count; index++ {
		gap := gaps[index]
		weight := 1 / (1 + gap)
		class := classes[index]
		total += weight
		score.analog += float64(class) * weight
		if class > 0 {
			bull += weight
		} else if class < 0 {
			bear += weight
		}
		gapSum += gap
	}
	if total == 0 {
		return score
	}
	score.analog /= total
	dir := 0
	if score.analog > 0.15 {
		dir = 1
	} else if score.analog < -0.15 {
		dir = -1
	}
	if dir == 1 {
		score.agree = bull / total
	} else if dir == -1 {
		score.agree = bear / total
	}
	avgGap := gapSum / float64(count)
	gapScale := (sumArray(weights[:]) * 0.45) + 0.000001
	score.tight = clampFloat(1-avgGap/gapScale, 0, 1)
	return score
}

func aiSourceGap(current [6]float64, row [6]float64, weights [6]float64) float64 {
	gap := 0.0
	for index := range current {
		gap += weights[index] * math.Log(1+absFloat(current[index]-row[index]))
	}
	return gap
}

func aiSourceRank(features [6]float64, score aiSourceScore, neural aiSourceNeuralState, cfg aiSourceConfig) float64 {
	neuralScore := aiSourceNeuralScore(features, neural)
	directional := absFloat(score.analog) / 3
	fullK := 0.0
	if score.count >= cfg.kNeighbors {
		fullK = 0.10
	}
	raw := directional*0.35 + score.agree*0.25 + score.tight*0.20 + normScoreFloat(neuralScore)*cfg.neuralInfluence + fullK
	return clampFloat(raw, 0, 1)
}

func aiSourceTrainNeural(state *aiSourceNeuralState, features [6]float64, outcome int, cfg aiSourceConfig) {
	target := 0.0
	if outcome > 0 {
		target = 1
	} else if outcome < 0 {
		target = -1
	}
	if target == 0 {
		return
	}
	prediction := aiSourceNeuralScore(features, *state)
	err := prediction - target
	grad := err
	if absFloat(err) > cfg.huberDelta {
		grad = cfg.huberDelta * signFloat(err)
	}
	state.step++
	for index := 0; index < 6; index++ {
		state.weights[index], state.mom[index], state.vel[index] = adamUpdate(state.weights[index], grad*features[index], state.mom[index], state.vel[index], state.step, cfg.learnRate)
	}
	state.bias, state.mom[6], state.vel[6] = adamUpdate(state.bias, grad, state.mom[6], state.vel[6], state.step, cfg.learnRate)
}

func aiSourceNeuralScore(features [6]float64, state aiSourceNeuralState) float64 {
	score := state.bias
	for index, value := range features {
		score += state.weights[index] * value
	}
	return score
}

func adamUpdate(weight float64, grad float64, mom float64, vel float64, step int, learnRate float64) (float64, float64, float64) {
	const beta1 = 0.9
	const beta2 = 0.999
	const eps = 0.00000001
	nextMom := beta1*mom + (1-beta1)*grad
	nextVel := beta2*vel + (1-beta2)*grad*grad
	mHat := nextMom / (1 - math.Pow(beta1, float64(step)))
	vHat := nextVel / (1 - math.Pow(beta2, float64(step)))
	return weight - learnRate*mHat/(math.Sqrt(vHat)+eps), nextMom, nextVel
}

func aiSourceBestID(scores [4]float64) int {
	best := 0
	for index := 1; index < len(scores); index++ {
		if scores[index] > scores[best] {
			best = index
		}
	}
	return best
}

func aiSourceName(id int) string {
	switch id {
	case aiSourceOpen:
		return "open"
	case aiSourceHigh:
		return "high"
	case aiSourceLow:
		return "low"
	default:
		return "close"
	}
}

func atrValueAt(index int, values []float64, offset int) float64 {
	atrIndex := index - offset
	if atrIndex < 0 || atrIndex >= len(values) {
		return 0
	}
	return values[atrIndex]
}

func emaLast(values []float64, period int) float64 {
	value, ok := ema(values, minInt(period, len(values)))
	if !ok {
		return values[len(values)-1]
	}
	return value
}

type aiSourceEMAState struct {
	period int
	sum    float64
	count  int
	value  float64
	ready  bool
}

func newAISourceEMAState(period int) *aiSourceEMAState {
	return &aiSourceEMAState{period: period}
}

func (s *aiSourceEMAState) append(value float64) float64 {
	if s == nil || s.period <= 1 {
		return value
	}
	if !s.ready {
		s.sum += value
		s.count++
		s.value = s.sum / float64(s.count)
		if s.count >= s.period {
			s.ready = true
		}
		return s.value
	}
	multiplier := 2 / float64(s.period+1)
	s.value = (value-s.value)*multiplier + s.value
	return s.value
}

func scaleValue01(value float64, low float64, high float64) float64 {
	if high == low {
		return 0.5
	}
	return clampFloat((value-low)/(high-low), 0, 1)
}

func clampFloat(value float64, low float64, high float64) float64 {
	return math.Max(low, math.Min(value, high))
}

func normScoreFloat(value float64) float64 {
	return 1 / (1 + math.Exp(-clampFloat(value, -8, 8)))
}

func sumArray(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total
}

func aiSourceBoolSignal(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

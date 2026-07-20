package indicatorcalc

import "math"

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

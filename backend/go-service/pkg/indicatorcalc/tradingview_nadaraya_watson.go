package indicatorcalc

import "math"

func addNadarayaWatsonEnvelopeFeatures(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, length int, bandwidth float64, multiplier float64) {
	middle, mae, previousMiddle, ok := nadarayaWatsonEnvelope(closes, length, bandwidth)
	if !ok {
		return
	}
	upper := middle + mae*multiplier
	lower := middle - mae*multiplier
	last := closes[len(closes)-1]
	setValueTarget(target, values, "nw_middle", middle, true)
	setValueTarget(target, values, "nw_upper", upper, true)
	setValueTarget(target, values, "nw_lower", lower, true)
	setValueTarget(target, values, "nw_width_pct", (upper-lower)/middle*100, middle != 0)
	setValueTarget(target, values, "nw_position", (last-lower)/(upper-lower), upper != lower)
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

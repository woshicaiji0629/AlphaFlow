package indicatorcalc

import "math"

type priceLevel struct {
	price   float64
	touches int
	recency int
	score   float64
}

func addSupportResistance(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	addSupportResistanceToSet(nil, values, signals, highs, lows, closes)
}

func addSupportResistanceToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	period := minInt(50, len(closes))
	if period < 5 {
		return
	}
	start := len(closes) - period
	last := closes[len(closes)-1]
	tolerance := supportResistanceTolerance(highs, lows, closes, period)
	supports, resistances := supportResistanceLevels(highs[start:], lows[start:], closes[start:], tolerance)
	if len(supports) == 0 || len(resistances) == 0 {
		resistance, support := highLow(highs[start:], lows[start:])
		supports = []priceLevel{{price: support, touches: 1, score: 1}}
		resistances = []priceLevel{{price: resistance, touches: 1, score: 1}}
	}
	support := supports[0].price
	resistance := resistances[0].price
	setValueTarget(target, values, "support_1", support, true)
	setValueTarget(target, values, "resistance_1", resistance, true)
	if len(supports) > 1 {
		setValueTarget(target, values, "support_2", supports[1].price, true)
	}
	if len(resistances) > 1 {
		setValueTarget(target, values, "resistance_2", resistances[1].price, true)
	}
	setValueTarget(target, values, "support_strength", supports[0].score, true)
	setValueTarget(target, values, "resistance_strength", resistances[0].score, true)
	setValueTarget(target, values, "support_distance_pct", percentDistance(last, support), support != 0)
	setValueTarget(target, values, "resistance_distance_pct", percentDistance(last, resistance), resistance != 0)
	switch {
	case support != 0 && math.Abs(last-support) <= tolerance:
		signals["sr_position"] = "near_support"
	case resistance != 0 && math.Abs(resistance-last) <= tolerance:
		signals["sr_position"] = "near_resistance"
	default:
		signals["sr_position"] = "mid"
	}
}

func supportResistanceLevels(highs []float64, lows []float64, closes []float64, tolerance float64) ([]priceLevel, []priceLevel) {
	last := closes[len(closes)-1]
	pivotHighs, pivotLows := pivots(highs, lows, 2)
	supports := clusterLevels(pivotLows, tolerance, last, false)
	resistances := clusterLevels(pivotHighs, tolerance, last, true)
	return supports, resistances
}

func pivots(highs []float64, lows []float64, width int) ([]priceLevel, []priceLevel) {
	pivotHighs := []priceLevel{}
	pivotLows := []priceLevel{}
	for index := width; index < len(highs)-width; index++ {
		isHigh := true
		isLow := true
		for offset := 1; offset <= width; offset++ {
			if highs[index] <= highs[index-offset] || highs[index] <= highs[index+offset] {
				isHigh = false
			}
			if lows[index] >= lows[index-offset] || lows[index] >= lows[index+offset] {
				isLow = false
			}
		}
		if isHigh {
			pivotHighs = append(pivotHighs, priceLevel{price: highs[index], touches: 1, recency: index})
		}
		if isLow {
			pivotLows = append(pivotLows, priceLevel{price: lows[index], touches: 1, recency: index})
		}
	}
	return pivotHighs, pivotLows
}

func clusterLevels(pivots []priceLevel, tolerance float64, last float64, resistance bool) []priceLevel {
	levels := []priceLevel{}
	for _, pivot := range pivots {
		merged := false
		for index := range levels {
			if math.Abs(levels[index].price-pivot.price) <= tolerance {
				touches := levels[index].touches + 1
				levels[index].price = (levels[index].price*float64(levels[index].touches) + pivot.price) / float64(touches)
				levels[index].touches = touches
				if pivot.recency > levels[index].recency {
					levels[index].recency = pivot.recency
				}
				merged = true
				break
			}
		}
		if !merged {
			levels = append(levels, pivot)
		}
	}
	filtered := levels[:0]
	for _, level := range levels {
		if resistance && level.price < last {
			continue
		}
		if !resistance && level.price > last {
			continue
		}
		distance := math.Abs(level.price - last)
		level.score = float64(level.touches)*2 + float64(level.recency)/10 - distance/math.Max(tolerance, 0.00000001)
		filtered = append(filtered, level)
	}
	sortLevels(filtered)
	return filtered
}

func supportResistanceTolerance(highs []float64, lows []float64, closes []float64, period int) float64 {
	atrValue, ok := atr(highs, lows, closes, minInt(14, period-1))
	last := closes[len(closes)-1]
	percent := last * 0.002
	if ok && atrValue > 0 {
		return math.Max(atrValue*0.35, percent)
	}
	return percent
}

func sortLevels(levels []priceLevel) {
	for i := 0; i < len(levels); i++ {
		for j := i + 1; j < len(levels); j++ {
			if levels[j].score > levels[i].score {
				levels[i], levels[j] = levels[j], levels[i]
			}
		}
	}
}

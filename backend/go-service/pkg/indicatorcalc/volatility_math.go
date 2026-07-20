package indicatorcalc

import "math"

func standardDeviation(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period {
		return 0, false
	}
	average, _ := sma(values, period)
	window := values[len(values)-period:]
	var variance float64
	for _, value := range window {
		diff := value - average
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(period)), true
}

func trueRangeSeries(highs []float64, lows []float64, closes []float64) []float64 {
	values := make([]float64, 0, len(closes))
	for index := range closes {
		if index == 0 {
			values = append(values, highs[index]-lows[index])
			continue
		}
		values = append(values, maxFloat(
			highs[index]-lows[index],
			absFloat(highs[index]-closes[index-1]),
			absFloat(lows[index]-closes[index-1]),
		))
	}
	return values
}

func linearRegression(values []float64, period int, offset int) (float64, bool) {
	if period <= 0 || len(values) < period || offset < 0 || offset >= period {
		return 0, false
	}
	window := values[len(values)-period:]
	var sumX float64
	var sumY float64
	var sumXY float64
	var sumXX float64
	for index, value := range window {
		x := float64(index)
		sumX += x
		sumY += value
		sumXY += x * value
		sumXX += x * x
	}
	count := float64(period)
	denominator := count*sumXX - sumX*sumX
	if denominator == 0 {
		return 0, false
	}
	slope := (count*sumXY - sumX*sumY) / denominator
	intercept := (sumY - slope*sumX) / count
	return intercept + slope*float64(period-1-offset), true
}

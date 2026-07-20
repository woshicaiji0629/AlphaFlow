package indicatorcalc

import "math"

func moneyFlowIndex(highs []float64, lows []float64, closes []float64, volumes []float64, period int) float64 {
	if len(closes) <= period {
		return 50
	}
	var positive float64
	var negative float64
	for index := len(closes) - period; index < len(closes); index++ {
		current := (highs[index] + lows[index] + closes[index]) / 3
		previous := (highs[index-1] + lows[index-1] + closes[index-1]) / 3
		flow := current * volumes[index]
		if current >= previous {
			positive += flow
		} else {
			negative += flow
		}
	}
	if negative == 0 {
		return 100
	}
	ratio := positive / negative
	return 100 - 100/(1+ratio)
}

func obvSeries(closes []float64, volumes []float64) []float64 {
	values := make([]float64, len(closes))
	for index := 1; index < len(closes); index++ {
		values[index] = values[index-1]
		switch {
		case closes[index] > closes[index-1]:
			values[index] += volumes[index]
		case closes[index] < closes[index-1]:
			values[index] -= volumes[index]
		}
	}
	return values
}

func priceVolumeTrendSeries(closes []float64, volumes []float64) []float64 {
	values := make([]float64, len(closes))
	for index := 1; index < len(closes); index++ {
		values[index] = values[index-1]
		if closes[index-1] != 0 {
			values[index] += (closes[index] - closes[index-1]) / closes[index-1] * volumes[index]
		}
	}
	return values
}

func moneyFlowStateValues(basic *basicIndicatorState, closes []float64) (float64, float64, float64, float64, float64, bool) {
	if len(closes) < 5 {
		return 0, 0, 0, 0, 0, false
	}
	_, obvSlope, pvt, pvtSlope, adLine, adLineSlope, ok := basic.moneyFlowValues()
	return obvSlope, pvt, pvtSlope, adLine, adLineSlope, ok
}

func rollingVWAP(highs []float64, lows []float64, closes []float64, volumes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) < period || len(volumes) != len(closes) {
		return 0, false
	}
	start := len(closes) - period
	var weighted float64
	var volumeSum float64
	for index := start; index < len(closes); index++ {
		typical := (highs[index] + lows[index] + closes[index]) / 3
		weighted += typical * volumes[index]
		volumeSum += volumes[index]
	}
	if volumeSum == 0 {
		return 0, false
	}
	return weighted / volumeSum, true
}

func accumulationDistributionSeries(highs []float64, lows []float64, closes []float64, volumes []float64) []float64 {
	values := make([]float64, len(closes))
	for index := range closes {
		flowVolume := moneyFlowVolume(highs[index], lows[index], closes[index], volumes[index])
		if index == 0 {
			values[index] = flowVolume
			continue
		}
		values[index] = values[index-1] + flowVolume
	}
	return values
}

func chaikinMoneyFlow(highs []float64, lows []float64, closes []float64, volumes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) < period || len(volumes) != len(closes) {
		return 0, false
	}
	start := len(closes) - period
	var flowSum float64
	var volumeSum float64
	for index := start; index < len(closes); index++ {
		flowSum += moneyFlowVolume(highs[index], lows[index], closes[index], volumes[index])
		volumeSum += volumes[index]
	}
	if volumeSum == 0 {
		return 0, false
	}
	return flowSum / volumeSum, true
}

func moneyFlowVolume(high float64, low float64, closeValue float64, volume float64) float64 {
	if high == low {
		return 0
	}
	multiplier := ((closeValue - low) - (high - closeValue)) / (high - low)
	return multiplier * volume
}

func volumePressure(closes []float64, volumes []float64, period int) float64 {
	if period <= 0 || len(closes) < period {
		return 0
	}
	start := len(closes) - period
	var upVolume float64
	var downVolume float64
	for index := start; index < len(closes); index++ {
		switch {
		case index > 0 && closes[index] > closes[index-1]:
			upVolume += volumes[index]
		case index > 0 && closes[index] < closes[index-1]:
			downVolume += volumes[index]
		}
	}
	total := upVolume + downVolume
	if total == 0 {
		return 0
	}
	return (upVolume - downVolume) / total
}

func volumeRatio(volumes []float64, period int) (float64, bool) {
	if period <= 0 || len(volumes) < period+1 {
		return 0, false
	}
	previous, ok := sma(volumes[:len(volumes)-1], period)
	if !ok || previous == 0 {
		return 0, false
	}
	return volumes[len(volumes)-1] / previous, true
}

func volumeBreakoutRatio(volumes []float64, period int) (float64, bool) {
	if period <= 0 || len(volumes) < period+1 {
		return 0, false
	}
	previousMax := volumes[len(volumes)-period-1]
	for _, volume := range volumes[len(volumes)-period-1 : len(volumes)-1] {
		if volume > previousMax {
			previousMax = volume
		}
	}
	if previousMax == 0 {
		return 0, false
	}
	return volumes[len(volumes)-1] / previousMax, true
}

func zScore(values []float64, period int) (float64, bool) {
	if period <= 1 || len(values) < period {
		return 0, false
	}
	window := values[len(values)-period:]
	mean := sum(window) / float64(period)
	var variance float64
	for _, value := range window {
		diff := value - mean
		variance += diff * diff
	}
	stddev := math.Sqrt(variance / float64(period))
	if stddev == 0 {
		return 0, true
	}
	return (values[len(values)-1] - mean) / stddev, true
}

func slope(values []float64, period int) float64 {
	if period <= 1 || len(values) < period {
		return 0
	}
	window := values[len(values)-period:]
	return window[len(window)-1] - window[0]
}

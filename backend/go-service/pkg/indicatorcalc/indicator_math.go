package indicatorcalc

import "math"

func sma(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period {
		return 0, false
	}
	var sum float64
	for _, value := range values[len(values)-period:] {
		sum += value
	}
	return sum / float64(period), true
}

func ema(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period {
		return 0, false
	}
	seed, _ := sma(values[:period], period)
	multiplier := 2 / float64(period+1)
	result := seed
	for _, value := range values[period:] {
		result = (value-result)*multiplier + result
	}
	return result, true
}

func emaSeries(values []float64, period int) ([]float64, bool) {
	if period <= 0 || len(values) < period {
		return nil, false
	}
	result := make([]float64, 0, len(values)-period+1)
	seed, _ := sma(values[:period], period)
	result = append(result, seed)
	multiplier := 2 / float64(period+1)
	current := seed
	for _, value := range values[period:] {
		current = (value-current)*multiplier + current
		result = append(result, current)
	}
	return result, true
}

func wma(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period {
		return 0, false
	}
	var weighted float64
	var weightSum float64
	window := values[len(values)-period:]
	for index, value := range window {
		weight := float64(index + 1)
		weighted += value * weight
		weightSum += weight
	}
	return weighted / weightSum, true
}

func bollinger(values []float64, period int, multiplier float64) (float64, float64, float64, bool) {
	middle, ok := sma(values, period)
	if !ok {
		return 0, 0, 0, false
	}
	window := values[len(values)-period:]
	var variance float64
	for _, value := range window {
		diff := value - middle
		variance += diff * diff
	}
	stddev := math.Sqrt(variance / float64(period))
	return middle + multiplier*stddev, middle, middle - multiplier*stddev, true
}

func obv(closes []float64, volumes []float64) float64 {
	var result float64
	for index := 1; index < len(closes); index++ {
		switch {
		case closes[index] > closes[index-1]:
			result += volumes[index]
		case closes[index] < closes[index-1]:
			result -= volumes[index]
		}
	}
	return result
}

func donchian(highs []float64, lows []float64, period int) (float64, float64, bool) {
	if len(highs) < period || len(lows) < period {
		return 0, 0, false
	}
	highest, lowest := highLow(highs[len(highs)-period:], lows[len(lows)-period:])
	return highest, lowest, true
}

func vwap(highs []float64, lows []float64, closes []float64, volumes []float64) float64 {
	var weighted float64
	var volumeSum float64
	for index := range closes {
		typical := (highs[index] + lows[index] + closes[index]) / 3
		weighted += typical * volumes[index]
		volumeSum += volumes[index]
	}
	if volumeSum == 0 {
		return closes[len(closes)-1]
	}
	return weighted / volumeSum
}

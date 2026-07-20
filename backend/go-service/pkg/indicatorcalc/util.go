package indicatorcalc

func percentDistance(value float64, base float64) float64 {
	if base == 0 {
		return 0
	}
	return (value - base) / base * 100
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func crossSignal(previousFast float64, previousSlow float64, currentFast float64, currentSlow float64) string {
	switch {
	case previousFast <= previousSlow && currentFast > currentSlow:
		return "golden"
	case previousFast >= previousSlow && currentFast < currentSlow:
		return "dead"
	default:
		return "none"
	}
}

func highestValue(values []float64) float64 {
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}

func lowestValue(values []float64) float64 {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

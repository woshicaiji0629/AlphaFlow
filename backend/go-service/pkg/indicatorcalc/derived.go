package indicatorcalc

import "math"

func addDerivedToSet(target *ValueSet, values map[string]string, opens []float64, highs []float64, lows []float64, closes []float64, volumes []float64, volumeMA float64, volumeMAOK bool) {
	last := len(closes) - 1
	open := opens[last]
	high := highs[last]
	low := lows[last]
	closeValue := closes[last]
	if open != 0 {
		setValueTarget(target, values, "change_pct", (closeValue-open)/open*100, true)
	}
	if closeValue != 0 {
		setValueTarget(target, values, "amplitude_pct", (high-low)/closeValue*100, true)
	}
	rangeValue := high - low
	if rangeValue != 0 {
		setValueTarget(target, values, "body_ratio", math.Abs(closeValue-open)/rangeValue, true)
		setValueTarget(target, values, "upper_shadow_ratio", (high-math.Max(open, closeValue))/rangeValue, true)
		setValueTarget(target, values, "lower_shadow_ratio", (math.Min(open, closeValue)-low)/rangeValue, true)
	}
	if volumeMAOK && volumeMA != 0 {
		setValueTarget(target, values, "volume_ratio20", volumes[last]/volumeMA, true)
	}
}

func trueRanges(highs []float64, lows []float64, closes []float64) []float64 {
	values := make([]float64, 0, len(closes)-1)
	for index := 1; index < len(closes); index++ {
		values = append(values, math.Max(
			highs[index]-lows[index],
			math.Max(math.Abs(highs[index]-closes[index-1]), math.Abs(lows[index]-closes[index-1])),
		))
	}
	return values
}

func highLow(highs []float64, lows []float64) (float64, float64) {
	highest := highs[0]
	lowest := lows[0]
	for index := range highs {
		if highs[index] > highest {
			highest = highs[index]
		}
		if lows[index] < lowest {
			lowest = lows[index]
		}
	}
	return highest, lowest
}

package indicatorwindow

import (
	"math"
	"strconv"
)

type numericStats struct {
	latest           float64
	previous         float64
	change           float64
	changePct        float64
	slope            float64
	direction        string
	risingCount      int
	fallingCount     int
	stableCount      int
	minimum          float64
	maximum          float64
	rangePositionPct float64
}

func addNumericSeriesAnalysis(
	values map[string]string,
	signals map[string]string,
	key string,
	series []float64,
) {
	stats := analyzeNumericSeries(series)
	prefix := key + "_win_"
	setValue(values, prefix+"latest", stats.latest, true)
	setValue(values, prefix+"previous", stats.previous, len(series) > 1)
	setValue(values, prefix+"change", stats.change, len(series) > 1)
	setValue(values, prefix+"change_pct", stats.changePct, len(series) > 1)
	setValue(values, prefix+"slope", stats.slope, len(series) > 1)
	setValue(values, prefix+"min", stats.minimum, true)
	setValue(values, prefix+"max", stats.maximum, true)
	setValue(values, prefix+"range_pos_pct", stats.rangePositionPct, stats.maximum != stats.minimum)
	values[prefix+"rising_count"] = strconv.Itoa(stats.risingCount)
	values[prefix+"falling_count"] = strconv.Itoa(stats.fallingCount)
	values[prefix+"stable_count"] = strconv.Itoa(stats.stableCount)
	signals[prefix+"direction"] = stats.direction
}

func numericSeries(points []point, key string) []float64 {
	series := make([]float64, 0, len(points))
	for _, point := range points {
		if value, ok := point.numericValues[key]; ok {
			series = append(series, value)
			continue
		}
		value, ok := point.values[key]
		if !ok {
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			continue
		}
		series = append(series, parsed)
	}
	return series
}

func analyzeNumericSeries(series []float64) numericStats {
	latest := series[len(series)-1]
	previous := latest
	if len(series) > 1 {
		previous = series[len(series)-2]
	}
	minimum := latest
	maximum := latest
	for _, value := range series {
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}
	change := latest - previous
	changePct := 0.0
	if previous != 0 {
		changePct = change / math.Abs(previous) * 100
	}
	slope := 0.0
	if len(series) > 1 {
		slope = (latest - series[0]) / float64(len(series)-1)
	}
	rangePosition := 0.0
	if maximum != minimum {
		rangePosition = (latest - minimum) / (maximum - minimum) * 100
	}
	return numericStats{
		latest:           latest,
		previous:         previous,
		change:           change,
		changePct:        changePct,
		slope:            slope,
		direction:        numericDirection(change),
		risingCount:      consecutiveNumericCount(series, 1),
		fallingCount:     consecutiveNumericCount(series, -1),
		stableCount:      consecutiveNumericCount(series, 0),
		minimum:          minimum,
		maximum:          maximum,
		rangePositionPct: rangePosition,
	}
}

func numericDirection(change float64) string {
	const epsilon = 1e-9
	switch {
	case change > epsilon:
		return "rising"
	case change < -epsilon:
		return "falling"
	default:
		return "flat"
	}
}

func consecutiveNumericCount(series []float64, sign int) int {
	if len(series) < 2 {
		return 0
	}
	count := 0
	for index := len(series) - 1; index > 0; index-- {
		direction := numericDirection(series[index] - series[index-1])
		if sign > 0 && direction != "rising" {
			break
		}
		if sign < 0 && direction != "falling" {
			break
		}
		if sign == 0 && direction != "flat" {
			break
		}
		count++
	}
	return count
}

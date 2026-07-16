package indicatorwindow

import (
	"math"
	"strconv"
)

type numericStats struct {
	count            int
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

type numericOutputFields struct {
	latest       string
	previous     string
	change       string
	changePct    string
	slope        string
	minimum      string
	maximum      string
	rangePosPct  string
	risingCount  string
	fallingCount string
	stableCount  string
	direction    string
}

func addNumericSeriesAnalysis(
	values map[string]string,
	signals map[string]string,
	key string,
	series []float64,
) {
	stats := analyzeNumericSeries(series)
	addNumericStatsAnalysis(&analysisContext{values: values, signals: signals, encodeValues: true}, key, stats)
}

func addNumericStatsAnalysis(
	ctx *analysisContext,
	key string,
	stats numericStats,
) {
	fields := ctx.numericOutputFields(key)
	ctx.setNumericValue(fields.latest, stats.latest, true)
	ctx.setNumericValue(fields.previous, stats.previous, stats.count > 1)
	ctx.setNumericValue(fields.change, stats.change, stats.count > 1)
	ctx.setNumericValue(fields.changePct, stats.changePct, stats.count > 1)
	ctx.setNumericValue(fields.slope, stats.slope, stats.count > 1)
	ctx.setNumericValue(fields.minimum, stats.minimum, true)
	ctx.setNumericValue(fields.maximum, stats.maximum, true)
	ctx.setNumericValue(fields.rangePosPct, stats.rangePositionPct, stats.maximum != stats.minimum)
	ctx.setNumericInt(fields.risingCount, stats.risingCount)
	ctx.setNumericInt(fields.fallingCount, stats.fallingCount)
	ctx.setNumericInt(fields.stableCount, stats.stableCount)
	ctx.signals[fields.direction] = stats.direction
}

func (ctx *analysisContext) numericOutputFields(key string) numericOutputFields {
	if ctx.numericFields != nil {
		if fields, ok := ctx.numericFields[key]; ok {
			return fields
		}
	}
	prefix := key + "_win_"
	fields := numericOutputFields{
		latest:       prefix + "latest",
		previous:     prefix + "previous",
		change:       prefix + "change",
		changePct:    prefix + "change_pct",
		slope:        prefix + "slope",
		minimum:      prefix + "min",
		maximum:      prefix + "max",
		rangePosPct:  prefix + "range_pos_pct",
		risingCount:  prefix + "rising_count",
		fallingCount: prefix + "falling_count",
		stableCount:  prefix + "stable_count",
		direction:    prefix + "direction",
	}
	if ctx.numericFields != nil {
		ctx.numericFields[key] = fields
	}
	return fields
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
		count:            len(series),
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

func numericStatsFromPoints(points []point, key string) (numericStats, bool) {
	stats := numericStats{}
	first := 0.0
	for _, point := range points {
		value, ok := numericPointValue(point, key)
		if !ok {
			continue
		}
		if stats.count == 0 {
			first = value
			stats.minimum = value
			stats.maximum = value
		} else {
			stats.previous = stats.latest
			stats.minimum = math.Min(stats.minimum, value)
			stats.maximum = math.Max(stats.maximum, value)
			direction := numericDirection(value - stats.latest)
			switch direction {
			case "rising":
				stats.risingCount++
				stats.fallingCount = 0
				stats.stableCount = 0
			case "falling":
				stats.risingCount = 0
				stats.fallingCount++
				stats.stableCount = 0
			default:
				stats.risingCount = 0
				stats.fallingCount = 0
				stats.stableCount++
			}
		}
		stats.latest = value
		stats.count++
	}
	if stats.count == 0 {
		return numericStats{}, false
	}
	if stats.count == 1 {
		stats.previous = stats.latest
	}
	stats.change = stats.latest - stats.previous
	if stats.previous != 0 {
		stats.changePct = stats.change / math.Abs(stats.previous) * 100
	}
	if stats.count > 1 {
		stats.slope = (stats.latest - first) / float64(stats.count-1)
	}
	if stats.maximum != stats.minimum {
		stats.rangePositionPct = (stats.latest - stats.minimum) / (stats.maximum - stats.minimum) * 100
	}
	stats.direction = numericDirection(stats.change)
	return stats, true
}

func (r *rollingWindow) numericStats(key string) (numericStats, bool) {
	index, ok := r.numericIndexes[key]
	if !ok || r.count == 0 {
		return numericStats{}, false
	}
	slot := &r.numericSlots[index]
	series := [DefaultLookback]float64{}
	seriesCount := 0
	start := r.next - r.count
	if start < 0 {
		start += DefaultLookback
	}
	for offset := 0; offset < r.count; offset++ {
		position := (start + offset) % DefaultLookback
		if slot.generations[position] != r.rowGenerations[position] {
			continue
		}
		series[seriesCount] = slot.values[position]
		seriesCount++
	}
	if seriesCount == 0 {
		return numericStats{}, false
	}
	return analyzeNumericSeries(series[:seriesCount]), true
}

func numericPointValue(point point, key string) (float64, bool) {
	if value, ok := point.numericValues[key]; ok {
		return value, true
	}
	value, ok := point.values[key]
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil
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

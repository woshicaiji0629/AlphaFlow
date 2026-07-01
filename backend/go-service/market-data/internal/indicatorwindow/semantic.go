package indicatorwindow

import (
	"math"
	"strconv"
	"strings"
)

func latestSignal(ctx *analysisContext, key string) (string, bool) {
	series := signalSeries(ctx.points, key)
	if len(series) == 0 {
		return "", false
	}
	return series[len(series)-1], true
}

func latestNumeric(ctx *analysisContext, key string) (float64, bool) {
	series := numericSeries(ctx.points, key)
	if len(series) == 0 {
		return 0, false
	}
	return series[len(series)-1], true
}

func numericStatsFor(ctx *analysisContext, key string) (numericStats, bool) {
	series := numericSeries(ctx.points, key)
	if len(series) == 0 {
		return numericStats{}, false
	}
	return analyzeNumericSeries(series), true
}

func signalStableCountFor(ctx *analysisContext, key string) int {
	series := signalSeries(ctx.points, key)
	if len(series) == 0 {
		return 0
	}
	return stableSignalCount(series)
}

func signalChangeCount(ctx *analysisContext, key string) int {
	series := signalSeries(ctx.points, key)
	if len(series) < 2 {
		return 0
	}
	count := 0
	for index := 1; index < len(series); index++ {
		if series[index] != series[index-1] {
			count++
		}
	}
	return count
}

func latestEventAge(ctx *analysisContext, key string, events ...string) (string, int, bool) {
	wanted := map[string]struct{}{}
	for _, event := range events {
		wanted[normalizeSignal(event)] = struct{}{}
	}
	series := signalSeries(ctx.points, key)
	for index := len(series) - 1; index >= 0; index-- {
		value := normalizeSignal(series[index])
		if _, ok := wanted[value]; ok {
			return value, len(series) - 1 - index, true
		}
	}
	return "", 0, false
}

func signalIs(value string, options ...string) bool {
	normalized := normalizeSignal(value)
	for _, option := range options {
		if normalized == normalizeSignal(option) {
			return true
		}
	}
	return false
}

func signalContains(value string, parts ...string) bool {
	normalized := normalizeSignal(value)
	for _, part := range parts {
		if strings.Contains(normalized, normalizeSignal(part)) {
			return true
		}
	}
	return false
}

func normalizeSignal(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func slopeLevel(value float64) string {
	absValue := math.Abs(value)
	switch {
	case absValue >= slopeSteepPct && value > 0:
		return "steep_up"
	case absValue >= slopeSteepPct && value < 0:
		return "steep_down"
	case absValue >= slopeWeakPct && value > 0:
		return "rising"
	case absValue >= slopeWeakPct && value < 0:
		return "falling"
	default:
		return "flat"
	}
}

func directionBias(direction string) string {
	switch {
	case signalIs(direction, "up", "bull", "bullish", "long"):
		return "bull"
	case signalIs(direction, "down", "bear", "bearish", "short"):
		return "bear"
	default:
		return "neutral"
	}
}

func boolSignal(value bool) string {
	return boolString(value)
}

func setInt(values map[string]string, key string, value int) {
	values[key] = strconv.Itoa(value)
}

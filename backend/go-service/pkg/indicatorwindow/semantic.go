package indicatorwindow

import (
	"math"
	"strconv"
	"strings"
)

func latestSignal(ctx *analysisContext, key string) (string, bool) {
	for index := len(ctx.points) - 1; index >= 0; index-- {
		if value, ok := ctx.points[index].signals[key]; ok {
			return value, true
		}
	}
	return "", false
}

func latestNumeric(ctx *analysisContext, key string) (float64, bool) {
	for index := len(ctx.points) - 1; index >= 0; index-- {
		point := ctx.points[index]
		if value, ok := point.numericValues[key]; ok {
			return value, true
		}
		value, ok := point.values[key]
		if !ok {
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func numericStatsFor(ctx *analysisContext, key string) (numericStats, bool) {
	return numericStatsFromPoints(ctx.points, key)
}

func signalStableCountFor(ctx *analysisContext, key string) int {
	stats, ok := signalStatsFromPoints(ctx.points, key)
	if !ok {
		return 0
	}
	return stats.stableCount
}

func signalChangeCount(ctx *analysisContext, key string) int {
	count := 0
	previous := ""
	hasPrevious := false
	for _, point := range ctx.points {
		value, ok := point.signals[key]
		if !ok {
			continue
		}
		if hasPrevious && value != previous {
			count++
		}
		previous = value
		hasPrevious = true
	}
	return count
}

func latestEventAge(ctx *analysisContext, key string, events ...string) (string, int, bool) {
	wanted := map[string]struct{}{}
	for _, event := range events {
		wanted[normalizeSignal(event)] = struct{}{}
	}
	age := 0
	for index := len(ctx.points) - 1; index >= 0; index-- {
		raw, exists := ctx.points[index].signals[key]
		if !exists {
			continue
		}
		value := normalizeSignal(raw)
		if _, ok := wanted[value]; ok {
			return value, age, true
		}
		age++
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

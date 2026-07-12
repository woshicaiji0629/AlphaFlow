package indicatorwindow

import "strconv"

type signalStats struct {
	count          int
	latest         string
	previous       string
	stableCount    int
	lastChangedAgo int
}

func addSignalSeriesAnalysis(
	values map[string]string,
	signals map[string]string,
	key string,
	series []string,
) {
	stats := analyzeSignalSeries(series)
	addSignalStatsAnalysis(values, signals, key, stats)
}

func addSignalStatsAnalysis(values map[string]string, signals map[string]string, key string, stats signalStats) {
	prefix := key + "_win_"
	signals[prefix+"latest"] = stats.latest
	if stats.previous != "" {
		signals[prefix+"previous"] = stats.previous
	}
	signals[prefix+"changed"] = boolString(stats.previous != "" && stats.latest != stats.previous)
	values[prefix+"stable_count"] = strconv.Itoa(stats.stableCount)
	values[prefix+"last_changed_ago"] = strconv.Itoa(stats.lastChangedAgo)
}

func analyzeSignalSeries(series []string) signalStats {
	latest := series[len(series)-1]
	previous := ""
	if len(series) > 1 {
		previous = series[len(series)-2]
	}
	return signalStats{
		count:          len(series),
		latest:         latest,
		previous:       previous,
		stableCount:    stableSignalCount(series),
		lastChangedAgo: lastSignalChangedAgo(series),
	}
}

func signalStatsFromPoints(points []point, key string) (signalStats, bool) {
	stats := signalStats{}
	changed := false
	for _, point := range points {
		value, ok := point.signals[key]
		if !ok {
			continue
		}
		stats.previous = stats.latest
		if stats.count == 0 || value != stats.latest {
			stats.stableCount = 1
			if stats.count > 0 {
				changed = true
				stats.lastChangedAgo = 1
			}
		} else {
			stats.stableCount++
			if stats.count > 0 {
				stats.lastChangedAgo++
			}
		}
		stats.latest = value
		stats.count++
	}
	if stats.count == 0 {
		return signalStats{}, false
	}
	if stats.count < 2 {
		stats.previous = ""
		stats.lastChangedAgo = 0
	} else if !changed {
		stats.lastChangedAgo = stats.count
	}
	return stats, true
}

func signalSeries(points []point, key string) []string {
	series := make([]string, 0, len(points))
	for _, point := range points {
		value, ok := point.signals[key]
		if !ok {
			continue
		}
		series = append(series, value)
	}
	return series
}

func stableSignalCount(series []string) int {
	if len(series) == 0 {
		return 0
	}
	latest := series[len(series)-1]
	count := 0
	for index := len(series) - 1; index >= 0; index-- {
		if series[index] != latest {
			break
		}
		count++
	}
	return count
}

func lastSignalChangedAgo(series []string) int {
	if len(series) < 2 {
		return 0
	}
	latest := series[len(series)-1]
	for index := len(series) - 2; index >= 0; index-- {
		if series[index] != latest {
			return len(series) - 1 - index
		}
	}
	return len(series)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

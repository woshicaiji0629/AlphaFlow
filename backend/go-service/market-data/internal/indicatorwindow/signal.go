package indicatorwindow

import "strconv"

func addSignalSeriesAnalysis(
	values map[string]string,
	signals map[string]string,
	key string,
	series []string,
) {
	latest := series[len(series)-1]
	previous := ""
	if len(series) > 1 {
		previous = series[len(series)-2]
	}
	prefix := key + "_win_"
	signals[prefix+"latest"] = latest
	if previous != "" {
		signals[prefix+"previous"] = previous
	}
	signals[prefix+"changed"] = boolString(previous != "" && latest != previous)
	values[prefix+"stable_count"] = strconv.Itoa(stableSignalCount(series))
	values[prefix+"last_changed_ago"] = strconv.Itoa(lastSignalChangedAgo(series))
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

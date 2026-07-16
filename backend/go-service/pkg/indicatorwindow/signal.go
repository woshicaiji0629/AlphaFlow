package indicatorwindow

type signalStats struct {
	count          int
	latest         string
	previous       string
	stableCount    int
	lastChangedAgo int
}

type signalOutputFields struct {
	latest         string
	previous       string
	changed        string
	stableCount    string
	lastChangedAgo string
}

func addSignalSeriesAnalysis(
	values map[string]string,
	signals map[string]string,
	key string,
	series []string,
) {
	stats := analyzeSignalSeries(series)
	addSignalStatsAnalysis(&analysisContext{values: values, signals: signals, encodeValues: true}, key, stats)
}

func addSignalStatsAnalysis(ctx *analysisContext, key string, stats signalStats) {
	fields := ctx.signalOutputFields(key)
	ctx.signals[fields.latest] = stats.latest
	if stats.previous != "" {
		ctx.signals[fields.previous] = stats.previous
	}
	ctx.signals[fields.changed] = boolString(stats.previous != "" && stats.latest != stats.previous)
	ctx.setNumericInt(fields.stableCount, stats.stableCount)
	ctx.setNumericInt(fields.lastChangedAgo, stats.lastChangedAgo)
}

func (ctx *analysisContext) signalOutputFields(key string) signalOutputFields {
	if ctx.signalFields != nil {
		if fields, ok := ctx.signalFields[key]; ok {
			return fields
		}
	}
	prefix := key + "_win_"
	fields := signalOutputFields{
		latest:         prefix + "latest",
		previous:       prefix + "previous",
		changed:        prefix + "changed",
		stableCount:    prefix + "stable_count",
		lastChangedAgo: prefix + "last_changed_ago",
	}
	if ctx.signalFields != nil {
		ctx.signalFields[key] = fields
	}
	return fields
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

func (r *rollingWindow) signalStats(key string) (signalStats, bool) {
	index, ok := r.signalIndexes[key]
	if !ok || r.count == 0 {
		return signalStats{}, false
	}
	slot := &r.signalSlots[index]
	series := [DefaultLookback]string{}
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
		return signalStats{}, false
	}
	return analyzeSignalSeries(series[:seriesCount]), true
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

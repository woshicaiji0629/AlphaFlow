package indicatorwindow

import "strconv"

func addGenericWindowAnalysis(ctx *analysisContext) {
	for _, key := range allNumericKeys(ctx.points) {
		ctx.addNumeric(key)
	}
	for _, key := range allSignalKeys(ctx.points) {
		ctx.addSignals(key)
	}
}

func allNumericKeys(points []point) []string {
	seen := map[string]struct{}{}
	for _, point := range points {
		for key, value := range point.values {
			if _, err := strconv.ParseFloat(value, 64); err == nil {
				seen[key] = struct{}{}
			}
		}
	}
	return sortedKeys(seen)
}

func allSignalKeys(points []point) []string {
	seen := map[string]struct{}{}
	for _, point := range points {
		for key := range point.signals {
			seen[key] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

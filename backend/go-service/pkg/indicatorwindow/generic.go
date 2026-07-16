package indicatorwindow

import "strconv"

func addGenericWindowAnalysis(ctx *analysisContext) {
	numericKeys := ctx.numericKeys
	if numericKeys == nil {
		numericKeys = allNumericKeys(ctx.points)
	}
	for _, key := range numericKeys {
		ctx.addNumeric(key)
	}
	signalKeys := ctx.signalKeys
	if signalKeys == nil {
		signalKeys = allSignalKeys(ctx.points)
	}
	for _, key := range signalKeys {
		ctx.addSignals(key)
	}
}

func allNumericKeys(points []point) []string {
	seen := map[string]struct{}{}
	for _, point := range points {
		for key := range point.numericValues {
			seen[key] = struct{}{}
		}
		for key, value := range point.values {
			if _, ok := point.numericValues[key]; ok {
				continue
			}
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

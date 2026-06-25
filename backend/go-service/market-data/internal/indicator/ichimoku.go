package indicator

func addIchimokuFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	current, previous, ok := ichimoku(highs, lows, closes)
	if !ok {
		return
	}
	setValue(values, "ichimoku_tenkan", current.tenkan, true)
	setValue(values, "ichimoku_kijun", current.kijun, true)
	setValue(values, "ichimoku_span_a", current.spanA, true)
	setValue(values, "ichimoku_span_b", current.spanB, true)
	last := closes[len(closes)-1]
	signals["ichimoku_trend"] = ichimokuTrend(current)
	signals["ichimoku_cloud"] = ichimokuCloud(last, current)
	signals["ichimoku_cross"] = crossSignal(previous.tenkan, previous.kijun, current.tenkan, current.kijun)
}

type ichimokuPoint struct {
	tenkan float64
	kijun  float64
	spanA  float64
	spanB  float64
}

func ichimoku(highs []float64, lows []float64, closes []float64) (ichimokuPoint, ichimokuPoint, bool) {
	if len(closes) < 53 {
		return ichimokuPoint{}, ichimokuPoint{}, false
	}
	current, ok := ichimokuAt(highs, lows, len(closes))
	if !ok {
		return ichimokuPoint{}, ichimokuPoint{}, false
	}
	previous, ok := ichimokuAt(highs, lows, len(closes)-1)
	if !ok {
		return ichimokuPoint{}, ichimokuPoint{}, false
	}
	return current, previous, true
}

func ichimokuAt(highs []float64, lows []float64, end int) (ichimokuPoint, bool) {
	if end < 52 || end > len(highs) || len(highs) != len(lows) {
		return ichimokuPoint{}, false
	}
	tenkanHigh, tenkanLow := highLow(highs[end-9:end], lows[end-9:end])
	kijunHigh, kijunLow := highLow(highs[end-26:end], lows[end-26:end])
	spanBHigh, spanBLow := highLow(highs[end-52:end], lows[end-52:end])
	tenkan := (tenkanHigh + tenkanLow) / 2
	kijun := (kijunHigh + kijunLow) / 2
	return ichimokuPoint{
		tenkan: tenkan,
		kijun:  kijun,
		spanA:  (tenkan + kijun) / 2,
		spanB:  (spanBHigh + spanBLow) / 2,
	}, true
}

func ichimokuTrend(point ichimokuPoint) string {
	switch {
	case point.tenkan > point.kijun && point.spanA > point.spanB:
		return "bull"
	case point.tenkan < point.kijun && point.spanA < point.spanB:
		return "bear"
	default:
		return "neutral"
	}
}

func ichimokuCloud(price float64, point ichimokuPoint) string {
	upper := maxFloat(point.spanA, point.spanB)
	lower := minFloat(point.spanA, point.spanB)
	switch {
	case price > upper:
		return "above_cloud"
	case price < lower:
		return "below_cloud"
	default:
		return "inside_cloud"
	}
}

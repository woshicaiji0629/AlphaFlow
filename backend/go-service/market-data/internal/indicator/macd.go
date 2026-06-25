package indicator

type macdPoint struct {
	value  float64
	signal float64
	hist   float64
}

func addMACDFeatures(values map[string]string, signals map[string]string, closes []float64, fast int, slow int, signal int) {
	series, ok := macdSeries(closes, fast, slow, signal)
	if !ok {
		return
	}
	last := series[len(series)-1]
	setValue(values, "macd_hist_delta", macdHistDelta(series), len(series) >= 2)
	setValue(values, "macd_zero_distance", last.value, true)
	signals["macd_cross"] = macdCross(series)
	signals["macd_zone"] = macdZone(last)
	signals["macd_momentum"] = macdMomentum(series)
}

func macd(values []float64, fast int, slow int, signal int) (float64, float64, float64, bool) {
	series, ok := macdSeries(values, fast, slow, signal)
	if !ok {
		return 0, 0, 0, false
	}
	last := series[len(series)-1]
	return last.value, last.signal, last.hist, true
}

func macdSeries(values []float64, fast int, slow int, signal int) ([]macdPoint, bool) {
	fastSeries, ok := emaSeries(values, fast)
	if !ok {
		return nil, false
	}
	slowSeries, ok := emaSeries(values, slow)
	if !ok {
		return nil, false
	}
	offset := len(fastSeries) - len(slowSeries)
	if offset < 0 {
		return nil, false
	}
	differences := make([]float64, 0, len(slowSeries))
	for index, slowValue := range slowSeries {
		differences = append(differences, fastSeries[index+offset]-slowValue)
	}
	signalSeries, ok := emaSeries(differences, signal)
	if !ok {
		return nil, false
	}
	offset = len(differences) - len(signalSeries)
	if offset < 0 {
		return nil, false
	}
	series := make([]macdPoint, 0, len(signalSeries))
	for index, signalValue := range signalSeries {
		value := differences[index+offset]
		series = append(series, macdPoint{
			value:  value,
			signal: signalValue,
			hist:   value - signalValue,
		})
	}
	return series, len(series) > 0
}

func macdHistDelta(series []macdPoint) float64 {
	if len(series) < 2 {
		return 0
	}
	last := len(series) - 1
	return series[last].hist - series[last-1].hist
}

func macdCross(series []macdPoint) string {
	if len(series) < 2 {
		return "none"
	}
	last := len(series) - 1
	previous := series[last-1]
	current := series[last]
	switch {
	case previous.value <= previous.signal && current.value > current.signal:
		return "golden"
	case previous.value >= previous.signal && current.value < current.signal:
		return "dead"
	default:
		return "none"
	}
}

func macdZone(point macdPoint) string {
	if point.value >= 0 {
		return "above_zero"
	}
	return "below_zero"
}

func macdMomentum(series []macdPoint) string {
	if len(series) < 2 {
		return "flat"
	}
	last := len(series) - 1
	current := series[last].hist
	previous := series[last-1].hist
	switch {
	case current > 0 && current >= previous:
		return "expanding_bull"
	case current > 0:
		return "fading_bull"
	case current < 0 && current <= previous:
		return "expanding_bear"
	case current < 0:
		return "fading_bear"
	default:
		return "flat"
	}
}

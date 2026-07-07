package indicatorcalc

type macdPoint struct {
	value  float64
	signal float64
	hist   float64
}

func addMACDFeatures(values map[string]string, signals map[string]string, closes []float64, fast int, slow int, signal int) {
	addMACDFeaturesWithPrefix(values, signals, closes, fast, slow, signal, "macd")
}

func addMACDFeaturesWithPrefix(
	values map[string]string,
	signals map[string]string,
	closes []float64,
	fast int,
	slow int,
	signal int,
	prefix string,
) {
	series, ok := macdSeries(closes, fast, slow, signal)
	if !ok {
		return
	}
	addMACDSeriesFeatures(values, signals, closes, series, prefix)
}

func addMACDSeriesFeatures(
	values map[string]string,
	signals map[string]string,
	closes []float64,
	series []macdPoint,
	prefix string,
) {
	if len(series) == 0 {
		return
	}
	last := series[len(series)-1]
	setValue(values, prefix, last.value, true)
	setValue(values, prefix+"_signal", last.signal, true)
	setValue(values, prefix+"_hist", last.hist, true)
	setValue(values, prefix+"_hist_delta", macdHistDelta(series), len(series) >= 2)
	setValue(values, prefix+"_zero_distance", last.value, true)
	signals[prefix+"_cross"] = macdCross(series)
	signals[prefix+"_zone"] = macdZone(last)
	signals[prefix+"_momentum"] = macdMomentum(series)
	signals[prefix+"_hist_phase"] = macdHistPhase(series)
	signals[prefix+"_signal_side"] = macdSignalSide(last)
	signals[prefix+"_divergence"] = macdDivergence(closes, series)
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

func macdHistPhase(series []macdPoint) string {
	if len(series) < 2 {
		return "unknown"
	}
	last := len(series) - 1
	current := series[last].hist
	previous := series[last-1].hist
	switch {
	case current > 0 && current > previous:
		return "above_rising"
	case current > 0:
		return "above_falling"
	case current <= 0 && current < previous:
		return "below_falling"
	default:
		return "below_rising"
	}
}

func macdSignalSide(point macdPoint) string {
	if point.value >= point.signal {
		return "above_signal"
	}
	return "below_signal"
}

func macdDivergence(closes []float64, series []macdPoint) string {
	if len(closes) < 30 || len(series) < 30 {
		return "none"
	}
	offset := len(closes) - len(series)
	priceWindow := closes[offset:]
	priceHighs, priceLows := valuePivots(priceWindow, 2)
	macdValues := make([]float64, len(series))
	for index, point := range series {
		macdValues[index] = point.hist
	}
	macdHighs, macdLows := valuePivots(macdValues, 2)
	if len(priceHighs) >= 2 && len(macdHighs) >= 2 {
		prevPrice, lastPrice := lastTwoSwings(priceHighs)
		prevMACD, lastMACD := nearestLevels(macdHighs, prevPrice.recency, lastPrice.recency)
		if lastPrice.price > prevPrice.price && lastMACD.price < prevMACD.price {
			return "bearish"
		}
	}
	if len(priceLows) >= 2 && len(macdLows) >= 2 {
		prevPrice, lastPrice := lastTwoSwings(priceLows)
		prevMACD, lastMACD := nearestLevels(macdLows, prevPrice.recency, lastPrice.recency)
		if lastPrice.price < prevPrice.price && lastMACD.price > prevMACD.price {
			return "bullish"
		}
	}
	return "none"
}

func valuePivots(values []float64, width int) ([]priceLevel, []priceLevel) {
	highs := []priceLevel{}
	lows := []priceLevel{}
	if width <= 0 || len(values) < width*2+1 {
		return highs, lows
	}
	for index := width; index < len(values)-width; index++ {
		isHigh := true
		isLow := true
		for offset := 1; offset <= width; offset++ {
			if values[index] <= values[index-offset] || values[index] <= values[index+offset] {
				isHigh = false
			}
			if values[index] >= values[index-offset] || values[index] >= values[index+offset] {
				isLow = false
			}
		}
		if isHigh {
			highs = append(highs, priceLevel{price: values[index], recency: index, touches: 1})
		}
		if isLow {
			lows = append(lows, priceLevel{price: values[index], recency: index, touches: 1})
		}
	}
	return highs, lows
}

func nearestLevels(levels []priceLevel, firstRecency int, secondRecency int) (priceLevel, priceLevel) {
	first := nearestLevel(levels, firstRecency)
	second := nearestLevel(levels, secondRecency)
	return first, second
}

func nearestLevel(levels []priceLevel, recency int) priceLevel {
	best := levels[0]
	bestDistance := absFloat(float64(levels[0].recency - recency))
	for _, level := range levels[1:] {
		distance := absFloat(float64(level.recency - recency))
		if distance < bestDistance {
			best = level
			bestDistance = distance
		}
	}
	return best
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

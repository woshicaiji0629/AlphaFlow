package indicatorcalc

func rsi(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) <= period {
		return 0, false
	}
	var avgGain float64
	var avgLoss float64
	for index := 1; index <= period; index++ {
		delta := values[index] - values[index-1]
		if delta >= 0 {
			avgGain += delta
		} else {
			avgLoss -= delta
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	for index := period + 1; index < len(values); index++ {
		delta := values[index] - values[index-1]
		gain := 0.0
		loss := 0.0
		if delta >= 0 {
			gain = delta
		} else {
			loss = -delta
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
	}
	if avgLoss == 0 {
		return 100, true
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs), true
}

func rsiSeries(values []float64, period int) ([]float64, bool) {
	if period <= 0 || len(values) <= period {
		return nil, false
	}
	series := make([]float64, 0, len(values)-period)
	var avgGain float64
	var avgLoss float64
	for index := 1; index <= period; index++ {
		delta := values[index] - values[index-1]
		if delta >= 0 {
			avgGain += delta
		} else {
			avgLoss -= delta
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	series = append(series, rsiFromAverages(avgGain, avgLoss))
	for index := period + 1; index < len(values); index++ {
		delta := values[index] - values[index-1]
		gain := 0.0
		loss := 0.0
		if delta >= 0 {
			gain = delta
		} else {
			loss = -delta
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		series = append(series, rsiFromAverages(avgGain, avgLoss))
	}
	return series, len(series) > 0
}

func rsiFromAverages(avgGain float64, avgLoss float64) float64 {
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}

func rsiDivergence(closes []float64, period int) string {
	series, ok := rsiSeries(closes, period)
	if !ok || len(closes) < 30 || len(series) < 16 {
		return "none"
	}
	return rsiDivergenceFromSeries(closes, series)
}

func rsiDivergenceFromSeries(closes []float64, series []float64) string {
	if len(closes) < 30 || len(series) < 16 {
		return "none"
	}
	offset := len(closes) - len(series)
	priceWindow := closes[offset:]
	priceHighs, priceLows := valuePivots(priceWindow, 2)
	rsiHighs, rsiLows := valuePivots(series, 2)
	if len(priceHighs) >= 2 && len(rsiHighs) >= 2 {
		prevPrice, lastPrice := lastTwoSwings(priceHighs)
		prevRSI, lastRSI := nearestLevels(rsiHighs, prevPrice.recency, lastPrice.recency)
		if lastPrice.price > prevPrice.price && lastRSI.price < prevRSI.price {
			return "bearish"
		}
	}
	if len(priceLows) >= 2 && len(rsiLows) >= 2 {
		prevPrice, lastPrice := lastTwoSwings(priceLows)
		prevRSI, lastRSI := nearestLevels(rsiLows, prevPrice.recency, lastPrice.recency)
		if lastPrice.price < prevPrice.price && lastRSI.price > prevRSI.price {
			return "bullish"
		}
	}
	return "none"
}

func rsiState(value float64) string {
	switch {
	case value >= 70:
		return "overbought"
	case value <= 30:
		return "oversold"
	case value >= 55:
		return "bull"
	case value <= 45:
		return "bear"
	default:
		return "neutral"
	}
}

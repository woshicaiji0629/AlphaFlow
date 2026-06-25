package indicator

import "math"

func addRSIFeatures(values map[string]string, signals map[string]string, closes []float64, period int) {
	value, ok := rsi(closes, period)
	setValue(values, "rsi14", value, ok)
	if !ok {
		return
	}
	previous, okPrevious := rsi(closes[:len(closes)-3], period)
	setValue(values, "rsi_slope3", value-previous, okPrevious)
	signals["rsi_state"] = rsiState(value)
	signals["rsi_divergence"] = rsiDivergence(closes, period)
}

func addOscillatorFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	k, d, j, ok := kdj(highs, lows, closes, 9)
	if ok {
		setValue(values, "kdj_k", k, true)
		setValue(values, "kdj_d", d, true)
		setValue(values, "kdj_j", j, true)
	}
	stochK, stochD, ok := stochastic(highs, lows, closes, 14, 3)
	if ok {
		setValue(values, "stoch_k", stochK, true)
		setValue(values, "stoch_d", stochD, true)
	}
	stochRSIK, stochRSID, ok := stochRSI(closes, 14, 14, 3)
	if ok {
		setValue(values, "stoch_rsi_k", stochRSIK, true)
		setValue(values, "stoch_rsi_d", stochRSID, true)
		signals["stoch_rsi_state"] = oscillatorState(stochRSIK, 80, 20)
	}
	skdjK, skdjD, previousK, previousD, ok := skdj(highs, lows, closes, 9, 3)
	if ok {
		setValue(values, "skdj_k", skdjK, true)
		setValue(values, "skdj_d", skdjD, true)
		signals["skdj_cross"] = crossSignal(previousK, previousD, skdjK, skdjD)
	}
	cciValue, ok := cci(highs, lows, closes, 20)
	if ok {
		setValue(values, "cci20", cciValue, true)
		signals["cci_state"] = oscillatorState(cciValue, 100, -100)
	}
	williamsValue, ok := williamsR(highs, lows, closes, 14)
	if ok {
		setValue(values, "williams_r14", williamsValue, true)
		signals["williams_state"] = williamsState(williamsValue)
	}
	rocValue, ok := roc(closes, 12)
	if ok {
		setValue(values, "roc12", rocValue, true)
		signals["roc_state"] = rocState(rocValue)
	}
}

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
	for end := period + 1; end <= len(values); end++ {
		value, ok := rsi(values[:end], period)
		if !ok {
			continue
		}
		series = append(series, value)
	}
	return series, len(series) > 0
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

func kdj(highs []float64, lows []float64, closes []float64, period int) (float64, float64, float64, bool) {
	if len(closes) < period {
		return 0, 0, 0, false
	}
	highest, lowest := highLow(highs[len(highs)-period:], lows[len(lows)-period:])
	if highest == lowest {
		return 50, 50, 50, true
	}
	rsv := (closes[len(closes)-1] - lowest) / (highest - lowest) * 100
	k := (2.0/3.0)*50 + (1.0/3.0)*rsv
	d := (2.0/3.0)*50 + (1.0/3.0)*k
	return k, d, 3*k - 2*d, true
}

func stochastic(highs []float64, lows []float64, closes []float64, period int, smooth int) (float64, float64, bool) {
	if len(closes) < period+smooth-1 {
		return 0, 0, false
	}
	values := make([]float64, 0, smooth)
	for offset := smooth - 1; offset >= 0; offset-- {
		end := len(closes) - offset
		start := end - period
		highest, lowest := highLow(highs[start:end], lows[start:end])
		if highest == lowest {
			values = append(values, 50)
			continue
		}
		values = append(values, (closes[end-1]-lowest)/(highest-lowest)*100)
	}
	d, _ := sma(values, smooth)
	return values[len(values)-1], d, true
}

func stochRSI(closes []float64, rsiPeriod int, stochPeriod int, smooth int) (float64, float64, bool) {
	if len(closes) <= rsiPeriod+stochPeriod+smooth {
		return 0, 0, false
	}
	values := make([]float64, 0, stochPeriod+smooth)
	for end := rsiPeriod + 1; end <= len(closes); end++ {
		value, ok := rsi(closes[:end], rsiPeriod)
		if !ok {
			continue
		}
		values = append(values, value)
	}
	if len(values) < stochPeriod+smooth {
		return 0, 0, false
	}
	kValues := make([]float64, 0, smooth)
	for offset := smooth - 1; offset >= 0; offset-- {
		end := len(values) - offset
		window := values[end-stochPeriod : end]
		highest := window[0]
		lowest := window[0]
		for _, value := range window[1:] {
			if value > highest {
				highest = value
			}
			if value < lowest {
				lowest = value
			}
		}
		if highest == lowest {
			kValues = append(kValues, 50)
			continue
		}
		kValues = append(kValues, (values[end-1]-lowest)/(highest-lowest)*100)
	}
	d, _ := sma(kValues, smooth)
	return kValues[len(kValues)-1], d, true
}

func skdj(highs []float64, lows []float64, closes []float64, period int, smooth int) (float64, float64, float64, float64, bool) {
	if len(closes) < period+smooth {
		return 0, 0, 0, 0, false
	}
	kValues := make([]float64, 0, smooth+1)
	for offset := smooth; offset >= 0; offset-- {
		end := len(closes) - offset
		start := end - period
		highest, lowest := highLow(highs[start:end], lows[start:end])
		value := 50.0
		if highest != lowest {
			value = (closes[end-1] - lowest) / (highest - lowest) * 100
		}
		kValues = append(kValues, value)
	}
	previousD, _ := sma(kValues[:smooth], smooth)
	currentD, _ := sma(kValues[1:], smooth)
	return kValues[len(kValues)-1], currentD, kValues[len(kValues)-2], previousD, true
}

func cci(highs []float64, lows []float64, closes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) < period {
		return 0, false
	}
	typicals := make([]float64, 0, period)
	for index := len(closes) - period; index < len(closes); index++ {
		typicals = append(typicals, (highs[index]+lows[index]+closes[index])/3)
	}
	mean := sum(typicals) / float64(period)
	var deviation float64
	for _, value := range typicals {
		deviation += math.Abs(value - mean)
	}
	deviation /= float64(period)
	if deviation == 0 {
		return 0, true
	}
	return (typicals[len(typicals)-1] - mean) / (0.015 * deviation), true
}

func williamsR(highs []float64, lows []float64, closes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) < period {
		return 0, false
	}
	highest, lowest := highLow(highs[len(highs)-period:], lows[len(lows)-period:])
	if highest == lowest {
		return -50, true
	}
	return (highest - closes[len(closes)-1]) / (highest - lowest) * -100, true
}

func roc(closes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) <= period {
		return 0, false
	}
	previous := closes[len(closes)-period-1]
	if previous == 0 {
		return 0, false
	}
	return (closes[len(closes)-1] - previous) / previous * 100, true
}

func oscillatorState(value float64, upper float64, lower float64) string {
	switch {
	case value >= upper:
		return "overbought"
	case value <= lower:
		return "oversold"
	default:
		return "neutral"
	}
}

func williamsState(value float64) string {
	switch {
	case value >= -20:
		return "overbought"
	case value <= -80:
		return "oversold"
	default:
		return "neutral"
	}
}

func rocState(value float64) string {
	switch {
	case value > 0.1:
		return "positive"
	case value < -0.1:
		return "negative"
	default:
		return "flat"
	}
}

func crossSignal(previousFast float64, previousSlow float64, currentFast float64, currentSlow float64) string {
	switch {
	case previousFast <= previousSlow && currentFast > currentSlow:
		return "golden"
	case previousFast >= previousSlow && currentFast < currentSlow:
		return "dead"
	default:
		return "none"
	}
}

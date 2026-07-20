package indicatorcalc

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
	k, d, ok := stochasticCompact(highs, lows, closes, period, smooth)
	if ok {
		return k, d, true
	}
	return stochasticBatch(highs, lows, closes, period, smooth)
}

func stochasticCompact(highs []float64, lows []float64, closes []float64, period int, smooth int) (float64, float64, bool) {
	if period <= 0 || smooth <= 0 || len(closes) < period+smooth-1 || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, 0, false
	}
	highWindow := newFloatMonotonicWindow(true)
	lowWindow := newFloatMonotonicWindow(false)
	if !highWindow.canHold(period) || !lowWindow.canHold(period) {
		return 0, 0, false
	}
	values := make([]float64, 0, smooth)
	firstEnd := len(closes) - smooth + 1
	warmupStart := firstEnd - period
	for index := warmupStart; index < len(closes); index++ {
		highWindow.push(index, highs[index])
		lowWindow.push(index, lows[index])
		end := index + 1
		if end < firstEnd {
			continue
		}
		highWindow.expireBefore(end - period)
		lowWindow.expireBefore(end - period)
		highest, okHigh := highWindow.value()
		lowest, okLow := lowWindow.value()
		if !okHigh || !okLow {
			return 0, 0, false
		}
		if highest == lowest {
			values = append(values, 50)
			continue
		}
		values = append(values, (closes[end-1]-lowest)/(highest-lowest)*100)
	}
	d, _ := sma(values, smooth)
	return values[len(values)-1], d, true
}

func stochasticBatch(highs []float64, lows []float64, closes []float64, period int, smooth int) (float64, float64, bool) {
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
	values, ok := rsiSeries(closes, rsiPeriod)
	if !ok {
		return 0, 0, false
	}
	return stochRSIFromSeries(values, stochPeriod, smooth)
}

func stochRSIFromSeries(values []float64, stochPeriod int, smooth int) (float64, float64, bool) {
	k, d, ok := stochRSIFromSeriesCompact(values, stochPeriod, smooth)
	if ok {
		return k, d, true
	}
	return stochRSIFromSeriesBatch(values, stochPeriod, smooth)
}

func stochRSIFromSeriesCompact(values []float64, stochPeriod int, smooth int) (float64, float64, bool) {
	if stochPeriod <= 0 || smooth <= 0 || len(values) < stochPeriod+smooth {
		return 0, 0, false
	}
	highWindow := newFloatMonotonicWindow(true)
	lowWindow := newFloatMonotonicWindow(false)
	if !highWindow.canHold(stochPeriod) || !lowWindow.canHold(stochPeriod) {
		return 0, 0, false
	}
	kValues := make([]float64, 0, smooth)
	firstEnd := len(values) - smooth + 1
	warmupStart := firstEnd - stochPeriod
	for index := warmupStart; index < len(values); index++ {
		highWindow.push(index, values[index])
		lowWindow.push(index, values[index])
		end := index + 1
		if end < firstEnd {
			continue
		}
		highWindow.expireBefore(end - stochPeriod)
		lowWindow.expireBefore(end - stochPeriod)
		highest, okHigh := highWindow.value()
		lowest, okLow := lowWindow.value()
		if !okHigh || !okLow {
			return 0, 0, false
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

func stochRSIFromSeriesBatch(values []float64, stochPeriod int, smooth int) (float64, float64, bool) {
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
	k, d, previousK, previousD, ok := skdjCompact(highs, lows, closes, period, smooth)
	if ok {
		return k, d, previousK, previousD, true
	}
	return skdjBatch(highs, lows, closes, period, smooth)
}

func skdjCompact(highs []float64, lows []float64, closes []float64, period int, smooth int) (float64, float64, float64, float64, bool) {
	if period <= 0 || smooth <= 0 || len(closes) < period+smooth || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, 0, 0, 0, false
	}
	highWindow := newFloatMonotonicWindow(true)
	lowWindow := newFloatMonotonicWindow(false)
	if !highWindow.canHold(period) || !lowWindow.canHold(period) {
		return 0, 0, 0, 0, false
	}
	kValues := make([]float64, 0, smooth+1)
	firstEnd := len(closes) - smooth
	warmupStart := firstEnd - period
	for index := warmupStart; index < len(closes); index++ {
		highWindow.push(index, highs[index])
		lowWindow.push(index, lows[index])
		end := index + 1
		if end < firstEnd {
			continue
		}
		highWindow.expireBefore(end - period)
		lowWindow.expireBefore(end - period)
		highest, okHigh := highWindow.value()
		lowest, okLow := lowWindow.value()
		if !okHigh || !okLow {
			return 0, 0, 0, 0, false
		}
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

func skdjBatch(highs []float64, lows []float64, closes []float64, period int, smooth int) (float64, float64, float64, float64, bool) {
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

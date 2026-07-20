package indicatorcalc

func addSqueezeMomentum(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	addSqueezeMomentumToSet(nil, values, signals, highs, lows, closes)
}

func addSqueezeMomentumToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	const (
		length   = 20
		multKC   = 1.5
		lengthKC = 20
	)
	basis, ok := sma(closes, length)
	if !ok {
		return
	}
	deviation, ok := standardDeviation(closes, length)
	if !ok {
		return
	}
	bbUpper := basis + multKC*deviation
	bbLower := basis - multKC*deviation

	ma, ok := sma(closes, lengthKC)
	if !ok {
		return
	}
	rangeMA, ok := recentTrueRangeMean(highs, lows, closes, lengthKC)
	if !ok {
		return
	}
	kcUpper := ma + rangeMA*multKC
	kcLower := ma - rangeMA*multKC
	squeeze := "off"
	switch {
	case bbLower > kcLower && bbUpper < kcUpper:
		squeeze = "on"
	case bbLower < kcLower && bbUpper > kcUpper:
		squeeze = "released"
	}
	signals["squeeze"] = squeeze
	momentum, previous, ok := squeezeMomentum(highs, lows, closes, lengthKC)
	if !ok {
		return
	}
	setValueTarget(target, values, "squeeze_momentum", momentum, true)
	setValueTarget(target, values, "squeeze_momentum_delta", momentum-previous, true)
	signals["squeeze_state"] = squeezeState(squeeze, momentum, previous)
	switch {
	case momentum > 0 && momentum >= previous:
		signals["momentum_state"] = "bull"
	case momentum > 0:
		signals["momentum_state"] = "bull_fading"
	case momentum < 0 && momentum <= previous:
		signals["momentum_state"] = "bear"
	case momentum < 0:
		signals["momentum_state"] = "bear_fading"
	default:
		signals["momentum_state"] = "flat"
	}
}

func recentTrueRangeMean(highs []float64, lows []float64, closes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) <= period || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, false
	}
	start := len(closes) - period
	total := 0.0
	for index := start; index < len(closes); index++ {
		total += maxFloat(
			highs[index]-lows[index],
			absFloat(highs[index]-closes[index-1]),
			absFloat(lows[index]-closes[index-1]),
		)
	}
	return total / float64(period), true
}

func squeezeState(squeeze string, momentum float64, previous float64) string {
	direction := "flat"
	switch {
	case momentum > 0 && momentum >= previous:
		direction = "up"
	case momentum < 0 && momentum <= previous:
		direction = "down"
	}
	switch squeeze {
	case "on":
		return "squeeze_on"
	case "released":
		return "release_" + direction
	default:
		return "off_" + direction
	}
}

func squeezeMomentum(highs []float64, lows []float64, closes []float64, period int) (float64, float64, bool) {
	if period <= 0 || len(closes) < period*2 || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, 0, false
	}
	current, ok := squeezeMomentumAt(highs, lows, closes, period, len(closes))
	if !ok {
		return 0, 0, false
	}
	previous, ok := squeezeMomentumAt(highs, lows, closes, period, len(closes)-1)
	if !ok {
		return 0, 0, false
	}
	return current, previous, true
}

func squeezeMomentumAt(highs []float64, lows []float64, closes []float64, period int, end int) (float64, bool) {
	value, ok := squeezeMomentumAtCompact(highs, lows, closes, period, end)
	if ok {
		return value, true
	}
	return squeezeMomentumAtBatch(highs, lows, closes, period, end)
}

func squeezeMomentumAtCompact(highs []float64, lows []float64, closes []float64, period int, end int) (float64, bool) {
	if end < period*2 || end > len(closes) || len(highs) < end || len(lows) < end {
		return 0, false
	}
	highWindow := newFloatMonotonicWindow(true)
	lowWindow := newFloatMonotonicWindow(false)
	if !highWindow.canHold(period) || !lowWindow.canHold(period) {
		return 0, false
	}
	start := end - period
	warmupStart := start - period + 1
	source := make([]float64, 0, period)
	closeSum := 0.0
	for index := warmupStart; index < end; index++ {
		highWindow.push(index, highs[index])
		lowWindow.push(index, lows[index])
		closeSum += closes[index]
		if index >= warmupStart+period {
			closeSum -= closes[index-period]
		}
		if index < start {
			continue
		}
		highWindow.expireBefore(index - period + 1)
		lowWindow.expireBefore(index - period + 1)
		highest, okHigh := highWindow.value()
		lowest, okLow := lowWindow.value()
		if !okHigh || !okLow {
			return 0, false
		}
		closeMA := closeSum / float64(period)
		baseline := ((highest+lowest)/2 + closeMA) / 2
		source = append(source, closes[index]-baseline)
	}
	return linearRegression(source, period, 0)
}

func squeezeMomentumAtBatch(highs []float64, lows []float64, closes []float64, period int, end int) (float64, bool) {
	if end < period*2 || end > len(closes) || len(highs) < end || len(lows) < end {
		return 0, false
	}
	source := make([]float64, 0, period)
	start := end - period
	for index := start; index < end; index++ {
		highest, lowest := highLow(highs[index-period+1:index+1], lows[index-period+1:index+1])
		closeMA, ok := sma(closes[:index+1], period)
		if !ok {
			return 0, false
		}
		baseline := ((highest+lowest)/2 + closeMA) / 2
		source = append(source, closes[index]-baseline)
	}
	return linearRegression(source, period, 0)
}

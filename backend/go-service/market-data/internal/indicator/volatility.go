package indicator

func addSqueezeMomentum(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	bbUpper, bbMiddle, bbLower, ok := bollinger(closes, 20, 2)
	if !ok {
		return
	}
	atrValue, ok := atr(highs, lows, closes, 20)
	if !ok {
		return
	}
	kcMiddle := bbMiddle
	kcUpper := kcMiddle + 1.5*atrValue
	kcLower := kcMiddle - 1.5*atrValue
	switch {
	case bbUpper < kcUpper && bbLower > kcLower:
		signals["squeeze"] = "on"
	case bbUpper > kcUpper && bbLower < kcLower:
		signals["squeeze"] = "released"
	default:
		signals["squeeze"] = "off"
	}
	momentum, previous, ok := squeezeMomentum(highs, lows, closes, 20)
	if !ok {
		return
	}
	setValue(values, "squeeze_momentum", momentum, true)
	setValue(values, "squeeze_momentum_delta", momentum-previous, true)
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

func addBollingerFeatures(values map[string]string, signals map[string]string, closes []float64) {
	upper, middle, lower, ok := bollinger(closes, 20, 2)
	if !ok || middle == 0 || upper == lower {
		return
	}
	last := closes[len(closes)-1]
	setValue(values, "bb_width_pct", (upper-lower)/middle*100, true)
	setValue(values, "bb_percent_b", (last-lower)/(upper-lower), true)
	switch {
	case last > upper:
		signals["bb_position"] = "above_upper"
	case last < lower:
		signals["bb_position"] = "below_lower"
	default:
		signals["bb_position"] = "inside"
	}
}

func squeezeMomentum(highs []float64, lows []float64, closes []float64, period int) (float64, float64, bool) {
	if period <= 0 || len(closes) < period+1 {
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
	if end < period || end > len(closes) {
		return 0, false
	}
	highest, lowest := highLow(highs[end-period:end], lows[end-period:end])
	closeMA, ok := sma(closes[:end], period)
	if !ok {
		return 0, false
	}
	baseline := ((highest+lowest)/2 + closeMA) / 2
	return closes[end-1] - baseline, true
}

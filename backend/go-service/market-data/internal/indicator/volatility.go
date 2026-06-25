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
	width := (upper - lower) / middle * 100
	setValue(values, "bb_width_pct", width, true)
	setValue(values, "bb_percent_b", (last-lower)/(upper-lower), true)
	addBollingerShapeFeatures(values, signals, closes, width)
	switch {
	case last > upper:
		signals["bb_position"] = "above_upper"
	case last < lower:
		signals["bb_position"] = "below_lower"
	default:
		signals["bb_position"] = "inside"
	}
}

func addBollingerShapeFeatures(values map[string]string, signals map[string]string, closes []float64, currentWidth float64) {
	if len(closes) < 25 {
		return
	}
	prevUpper, prevMiddle, prevLower, ok := bollinger(closes[:len(closes)-5], 20, 2)
	if !ok || prevMiddle == 0 {
		return
	}
	upper, middle, lower, ok := bollinger(closes, 20, 2)
	if !ok {
		return
	}
	previousWidth := (prevUpper - prevLower) / prevMiddle * 100
	widthDelta := currentWidth - previousWidth
	setValue(values, "bb_width_delta", widthDelta, true)
	setValue(values, "bb_middle_slope_pct", percentDistance(middle, prevMiddle), prevMiddle != 0)
	setValue(values, "bb_upper_slope_pct", percentDistance(upper, prevUpper), prevUpper != 0)
	setValue(values, "bb_lower_slope_pct", percentDistance(lower, prevLower), prevLower != 0)
	signals["bb_width_state"] = bollingerWidthState(widthDelta, previousWidth)
	signals["bb_trend"] = bollingerTrend(percentDistance(middle, prevMiddle))
}

func bollingerWidthState(delta float64, previousWidth float64) string {
	threshold := 0.05
	if previousWidth > 0 {
		threshold = previousWidth * 0.08
	}
	switch {
	case delta > threshold:
		return "expanding"
	case delta < -threshold:
		return "contracting"
	default:
		return "flat"
	}
}

func bollingerTrend(middleSlopePct float64) string {
	switch {
	case middleSlopePct > 0.08:
		return "up"
	case middleSlopePct < -0.08:
		return "down"
	default:
		return "flat"
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

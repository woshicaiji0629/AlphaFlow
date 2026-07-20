package indicatorcalc

func addBollingerFeatures(values map[string]string, signals map[string]string, closes []float64) {
	addBollingerFeaturesWithContext(values, signals, closes, nil)
}

func addBollingerFeaturesWithContext(values map[string]string, signals map[string]string, closes []float64, features *featureContext) {
	addBollingerFeaturesWithContextToSet(nil, values, signals, closes, features)
}

func addBollingerFeaturesWithContextToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, features *featureContext) {
	upper, middle, lower, ok := 0.0, 0.0, 0.0, false
	if features != nil {
		upper, middle, lower, ok = features.bollinger(20, 2)
	} else {
		upper, middle, lower, ok = bollinger(closes, 20, 2)
	}
	if !ok || middle == 0 || upper == lower {
		return
	}
	last := closes[len(closes)-1]
	width := (upper - lower) / middle * 100
	setValueTarget(target, values, "bb_width_pct", width, true)
	setValueTarget(target, values, "bb_percent_b", (last-lower)/(upper-lower), true)
	addBollingerShapeFeaturesToSet(target, values, signals, closes, width)
	switch {
	case last > upper:
		signals["bb_position"] = "above_upper"
	case last < lower:
		signals["bb_position"] = "below_lower"
	default:
		signals["bb_position"] = "inside"
	}
}

func addBollingerShapeFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, currentWidth float64) {
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
	setValueTarget(target, values, "bb_width_delta", widthDelta, true)
	setValueTarget(target, values, "bb_middle_slope_pct", percentDistance(middle, prevMiddle), prevMiddle != 0)
	setValueTarget(target, values, "bb_upper_slope_pct", percentDistance(upper, prevUpper), prevUpper != 0)
	setValueTarget(target, values, "bb_lower_slope_pct", percentDistance(lower, prevLower), prevLower != 0)
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

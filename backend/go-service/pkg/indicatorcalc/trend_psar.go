package indicatorcalc

func addPSARFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	addPSARFeaturesToSet(nil, values, signals, highs, lows, closes)
}

func addPSARFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	value, direction, ok := psar(highs, lows, closes, 0.02, 0.2)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValueTarget(target, values, "psar", value, true)
	setValueTarget(target, values, "psar_distance_pct", percentDistance(last, value), value != 0)
	signals["psar_direction"] = direction
}

func psar(highs []float64, lows []float64, closes []float64, step float64, maxStep float64) (float64, string, bool) {
	if len(closes) < 3 || step <= 0 || maxStep < step {
		return 0, "", false
	}
	uptrend := closes[1] >= closes[0]
	sar := lows[0]
	ep := highs[0]
	if !uptrend {
		sar = highs[0]
		ep = lows[0]
	}
	acceleration := step
	for index := 1; index < len(closes); index++ {
		sar = sar + acceleration*(ep-sar)
		if uptrend {
			if index >= 2 {
				sar = minFloat(sar, lows[index-1], lows[index-2])
			}
			if lows[index] < sar {
				uptrend = false
				sar = ep
				ep = lows[index]
				acceleration = step
				continue
			}
			if highs[index] > ep {
				ep = highs[index]
				acceleration = minFloat(acceleration+step, maxStep)
			}
			continue
		}
		if index >= 2 {
			sar = maxFloat(sar, highs[index-1], highs[index-2])
		}
		if highs[index] > sar {
			uptrend = true
			sar = ep
			ep = highs[index]
			acceleration = step
			continue
		}
		if lows[index] < ep {
			ep = lows[index]
			acceleration = minFloat(acceleration+step, maxStep)
		}
	}
	if uptrend {
		return sar, "up", true
	}
	return sar, "down", true
}

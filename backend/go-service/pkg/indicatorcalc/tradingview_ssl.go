package indicatorcalc

func addSSLChannelFeatures(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int) {
	upper, lower, direction, previousDirection, ok := sslChannel(highs, lows, closes, period)
	if !ok {
		return
	}
	setValueTarget(target, values, "ssl_upper", upper, true)
	setValueTarget(target, values, "ssl_lower", lower, true)
	setValueTarget(target, values, "ssl_width_pct", (upper-lower)/closes[len(closes)-1]*100, closes[len(closes)-1] != 0)
	signals["ssl_direction"] = direction
	signals["ssl_cross"] = directionFlipCross(previousDirection, direction)
}

func sslChannel(highs []float64, lows []float64, closes []float64, period int) (float64, float64, string, string, bool) {
	if period <= 0 || len(closes) < period+1 || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, 0, "", "", false
	}
	direction := "neutral"
	previousDirection := direction
	var upper float64
	var lower float64
	for end := period; end <= len(closes); end++ {
		highMA, okHigh := sma(highs[:end], period)
		lowMA, okLow := sma(lows[:end], period)
		if !okHigh || !okLow {
			return 0, 0, "", "", false
		}
		previousDirection = direction
		switch {
		case closes[end-1] > highMA:
			direction = "bull"
		case closes[end-1] < lowMA:
			direction = "bear"
		}
		if direction == "bear" {
			upper, lower = lowMA, highMA
		} else {
			upper, lower = highMA, lowMA
		}
	}
	return upper, lower, direction, previousDirection, true
}

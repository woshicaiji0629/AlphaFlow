package indicatorcalc

func addTrendFeaturesWithContext(values map[string]string, signals map[string]string, closes []float64, features *featureContext) {
	addTrendFeaturesWithContextToSet(nil, values, signals, closes, features)
}

func addTrendFeaturesWithContextToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, features *featureContext) {
	var ema7, ema25, ema99 float64
	var ok7, ok25, ok99 bool
	if features != nil {
		ema7, ok7 = features.emaValue(7)
		ema25, ok25 = features.emaValue(25)
		ema99, ok99 = features.emaValue(99)
	} else {
		ema7, ok7 = ema(closes, 7)
		ema25, ok25 = ema(closes, 25)
		ema99, ok99 = ema(closes, 99)
	}
	last := closes[len(closes)-1]
	if ok7 && ok25 && ok99 {
		setValueTarget(target, values, "price_ema7_distance_pct", percentDistance(last, ema7), true)
		setValueTarget(target, values, "price_ema25_distance_pct", percentDistance(last, ema25), true)
		setValueTarget(target, values, "price_ema99_distance_pct", percentDistance(last, ema99), true)
		switch {
		case ema7 > ema25 && ema25 > ema99:
			signals["ema_alignment"] = "bull"
		case ema7 < ema25 && ema25 < ema99:
			signals["ema_alignment"] = "bear"
		default:
			signals["ema_alignment"] = "mixed"
		}
	}
	if len(closes) >= 35 {
		recent, okRecent := ema25, ok25
		var prev float64
		var okPrev bool
		if features != nil {
			prev, okPrev = features.emaHistoricalValue(25, 5)
		} else {
			prev, okPrev = ema(closes[:len(closes)-5], 25)
		}
		if okRecent && okPrev && prev != 0 {
			slope := (recent - prev) / prev * 100
			setValueTarget(target, values, "ema25_slope5_pct", slope, true)
			switch {
			case slope > 0.15:
				signals["trend_direction"] = "up"
			case slope < -0.15:
				signals["trend_direction"] = "down"
			default:
				signals["trend_direction"] = "range"
			}
		}
	}
}

func minFloat(first float64, values ...float64) float64 {
	result := first
	for _, value := range values {
		if value < result {
			result = value
		}
	}
	return result
}

func maxFloat(first float64, values ...float64) float64 {
	result := first
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func trendFlip(previous string, current string) string {
	if previous == "" || current == "" || previous == current {
		return "none"
	}
	return current
}

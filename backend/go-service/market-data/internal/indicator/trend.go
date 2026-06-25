package indicator

func addTrendFeatures(values map[string]string, signals map[string]string, closes []float64) {
	ema7, ok7 := ema(closes, 7)
	ema25, ok25 := ema(closes, 25)
	ema99, ok99 := ema(closes, 99)
	last := closes[len(closes)-1]
	if ok7 && ok25 && ok99 {
		setValue(values, "price_ema7_distance_pct", percentDistance(last, ema7), true)
		setValue(values, "price_ema25_distance_pct", percentDistance(last, ema25), true)
		setValue(values, "price_ema99_distance_pct", percentDistance(last, ema99), true)
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
		recent, okRecent := ema(closes, 25)
		prev, okPrev := ema(closes[:len(closes)-5], 25)
		if okRecent && okPrev && prev != 0 {
			slope := (recent - prev) / prev * 100
			setValue(values, "ema25_slope5_pct", slope, true)
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

func addSupertrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64) {
	value, direction, ok := supertrend(highs, lows, closes, period, multiplier)
	if !ok {
		return
	}
	setValue(values, "supertrend", value, true)
	signals["supertrend_direction"] = direction
}

func addAlphaTrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) {
	value, mfi, direction, ok := alphaTrend(highs, lows, closes, volumes, period, multiplier)
	if !ok {
		return
	}
	setValue(values, "alphatrend", value, true)
	setValue(values, "mfi14", mfi, true)
	signals["alphatrend_direction"] = direction
}

func supertrend(highs []float64, lows []float64, closes []float64, period int, multiplier float64) (float64, string, bool) {
	if period <= 0 || len(closes) <= period {
		return 0, "", false
	}
	trs := trueRanges(highs, lows, closes)
	if len(trs) < period {
		return 0, "", false
	}
	atrValues := make([]float64, len(closes))
	firstATR, _ := sma(trs[:period], period)
	atrValues[period] = firstATR
	for index := period + 1; index < len(closes); index++ {
		atrValues[index] = (atrValues[index-1]*float64(period-1) + trs[index-1]) / float64(period)
	}

	finalUpper := make([]float64, len(closes))
	finalLower := make([]float64, len(closes))
	direction := make([]string, len(closes))
	for index := period; index < len(closes); index++ {
		mid := (highs[index] + lows[index]) / 2
		basicUpper := mid + multiplier*atrValues[index]
		basicLower := mid - multiplier*atrValues[index]
		if index == period {
			finalUpper[index] = basicUpper
			finalLower[index] = basicLower
			if closes[index] >= mid {
				direction[index] = "up"
			} else {
				direction[index] = "down"
			}
			continue
		}
		if basicUpper < finalUpper[index-1] || closes[index-1] > finalUpper[index-1] {
			finalUpper[index] = basicUpper
		} else {
			finalUpper[index] = finalUpper[index-1]
		}
		if basicLower > finalLower[index-1] || closes[index-1] < finalLower[index-1] {
			finalLower[index] = basicLower
		} else {
			finalLower[index] = finalLower[index-1]
		}
		switch {
		case direction[index-1] == "down" && closes[index] > finalUpper[index]:
			direction[index] = "up"
		case direction[index-1] == "up" && closes[index] < finalLower[index]:
			direction[index] = "down"
		default:
			direction[index] = direction[index-1]
		}
	}
	last := len(closes) - 1
	if direction[last] == "down" {
		return finalUpper[last], "down", true
	}
	return finalLower[last], "up", true
}

func alphaTrend(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) (float64, float64, string, bool) {
	if period <= 0 || len(closes) <= period || len(volumes) != len(closes) {
		return 0, 0, "", false
	}
	trs := trueRanges(highs, lows, closes)
	if len(trs) < period {
		return 0, 0, "", false
	}
	trend := make([]float64, len(closes))
	direction := "range"
	for index := period; index < len(closes); index++ {
		atrValue, _ := sma(trs[index-period:index], period)
		mfi := moneyFlowIndex(highs[:index+1], lows[:index+1], closes[:index+1], volumes[:index+1], period)
		up := lows[index] - multiplier*atrValue
		down := highs[index] + multiplier*atrValue
		if index == period {
			if mfi >= 50 {
				trend[index] = up
				direction = "up"
			} else {
				trend[index] = down
				direction = "down"
			}
			continue
		}
		if mfi >= 50 {
			if up < trend[index-1] {
				trend[index] = trend[index-1]
			} else {
				trend[index] = up
			}
			direction = "up"
		} else {
			if down > trend[index-1] {
				trend[index] = trend[index-1]
			} else {
				trend[index] = down
			}
			direction = "down"
		}
	}
	lastMFI := moneyFlowIndex(highs, lows, closes, volumes, period)
	return trend[len(closes)-1], lastMFI, direction, true
}

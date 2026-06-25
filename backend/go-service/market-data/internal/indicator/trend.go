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
	points, ok := supertrendSeries(highs, lows, closes, period, multiplier)
	if !ok {
		return
	}
	lastIndex := len(points) - 1
	lastPoint := points[lastIndex]
	lastClose := closes[len(closes)-1]
	setValue(values, "supertrend", lastPoint.value, true)
	setValue(values, "supertrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValue(values, "supertrend_stop_distance_pct", absFloat(percentDistance(lastClose, lastPoint.value)), lastPoint.value != 0)
	signals["supertrend_direction"] = lastPoint.direction
	signals["supertrend_flip"] = trendFlip(points[lastIndex-1].direction, lastPoint.direction)

	for _, preset := range []struct {
		name       string
		period     int
		multiplier float64
	}{
		{name: "supertrend_7_2", period: 7, multiplier: 2},
		{name: "supertrend_10_3", period: 10, multiplier: 3},
		{name: "supertrend_14_4", period: 14, multiplier: 4},
	} {
		presetValue, presetDirection, presetOK := supertrend(highs, lows, closes, preset.period, preset.multiplier)
		if !presetOK {
			continue
		}
		setValue(values, preset.name, presetValue, true)
		signals[preset.name+"_direction"] = presetDirection
	}
}

func addAlphaTrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) {
	points, lastMFI, ok := alphaTrendSeries(highs, lows, closes, volumes, period, multiplier)
	if !ok {
		return
	}
	lastIndex := len(points) - 1
	lastPoint := points[lastIndex]
	prevPoint := points[lastIndex-1]
	lastClose := closes[len(closes)-1]
	setValue(values, "alphatrend", lastPoint.value, true)
	setValue(values, "mfi14", lastMFI, true)
	setValue(values, "alphatrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValue(values, "alphatrend_slope_pct", percentDistance(lastPoint.value, prevPoint.value), prevPoint.value != 0)
	signals["alphatrend_direction"] = lastPoint.direction
	signals["alphatrend_flip"] = trendFlip(prevPoint.direction, lastPoint.direction)
}

func addPSARFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	value, direction, ok := psar(highs, lows, closes, 0.02, 0.2)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValue(values, "psar", value, true)
	setValue(values, "psar_distance_pct", percentDistance(last, value), value != 0)
	signals["psar_direction"] = direction
}

func supertrend(highs []float64, lows []float64, closes []float64, period int, multiplier float64) (float64, string, bool) {
	points, ok := supertrendSeries(highs, lows, closes, period, multiplier)
	if !ok {
		return 0, "", false
	}
	last := points[len(points)-1]
	return last.value, last.direction, true
}

type trendPoint struct {
	value     float64
	direction string
}

func supertrendSeries(highs []float64, lows []float64, closes []float64, period int, multiplier float64) ([]trendPoint, bool) {
	if period <= 0 || len(closes) <= period {
		return nil, false
	}
	trs := trueRanges(highs, lows, closes)
	if len(trs) < period {
		return nil, false
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
	points := make([]trendPoint, 0, len(closes)-period)
	for index := period; index < len(closes); index++ {
		if direction[index] == "down" {
			points = append(points, trendPoint{value: finalUpper[index], direction: "down"})
			continue
		}
		points = append(points, trendPoint{value: finalLower[index], direction: "up"})
	}
	if len(points) < 2 {
		return nil, false
	}
	return points, true
}

func alphaTrend(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) (float64, float64, string, bool) {
	points, lastMFI, ok := alphaTrendSeries(highs, lows, closes, volumes, period, multiplier)
	if !ok {
		return 0, 0, "", false
	}
	last := points[len(points)-1]
	return last.value, lastMFI, last.direction, true
}

func alphaTrendSeries(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) ([]trendPoint, float64, bool) {
	if period <= 0 || len(closes) <= period || len(volumes) != len(closes) {
		return nil, 0, false
	}
	trs := trueRanges(highs, lows, closes)
	if len(trs) < period {
		return nil, 0, false
	}
	trend := make([]float64, len(closes))
	directions := make([]string, len(closes))
	for index := period; index < len(closes); index++ {
		atrValue, _ := sma(trs[index-period:index], period)
		mfi := moneyFlowIndex(highs[:index+1], lows[:index+1], closes[:index+1], volumes[:index+1], period)
		up := lows[index] - multiplier*atrValue
		down := highs[index] + multiplier*atrValue
		if index == period {
			if mfi >= 50 {
				trend[index] = up
				directions[index] = "up"
			} else {
				trend[index] = down
				directions[index] = "down"
			}
			continue
		}
		if mfi >= 50 {
			if up < trend[index-1] {
				trend[index] = trend[index-1]
			} else {
				trend[index] = up
			}
			directions[index] = "up"
		} else {
			if down > trend[index-1] {
				trend[index] = trend[index-1]
			} else {
				trend[index] = down
			}
			directions[index] = "down"
		}
	}
	lastMFI := moneyFlowIndex(highs, lows, closes, volumes, period)
	points := make([]trendPoint, 0, len(closes)-period)
	for index := period; index < len(closes); index++ {
		points = append(points, trendPoint{value: trend[index], direction: directions[index]})
	}
	if len(points) < 2 {
		return nil, 0, false
	}
	return points, lastMFI, true
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

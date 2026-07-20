package indicatorcalc

func addAlphaTrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) {
	addAlphaTrendToSet(nil, values, signals, highs, lows, closes, volumes, period, multiplier)
}

func addAlphaTrendToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) {
	points, lastMFI, ok := alphaTrendSeries(highs, lows, closes, volumes, period, multiplier)
	if !ok {
		return
	}
	lastIndex := len(points) - 1
	lastPoint := points[lastIndex]
	prevPoint := points[lastIndex-1]
	lastClose := closes[len(closes)-1]
	setValueTarget(target, values, "alphatrend", lastPoint.value, true)
	setValueTarget(target, values, "mfi14", lastMFI, true)
	setValueTarget(target, values, "alphatrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValueTarget(target, values, "alphatrend_slope_pct", percentDistance(lastPoint.value, prevPoint.value), prevPoint.value != 0)
	signals["alphatrend_direction"] = lastPoint.direction.String()
	signals["alphatrend_flip"] = trendFlip(prevPoint.direction.String(), lastPoint.direction.String())
	cross, signal := alphaTrendSignals(points)
	signals["alphatrend_cross"] = cross
	signals["alphatrend_signal"] = signal
}

func alphaTrend(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) (float64, float64, string, bool) {
	points, lastMFI, ok := alphaTrendSeries(highs, lows, closes, volumes, period, multiplier)
	if !ok {
		return 0, 0, "", false
	}
	last := points[len(points)-1]
	return last.value, lastMFI, last.direction.String(), true
}

func alphaTrendSeries(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) ([]trendPoint, float64, bool) {
	points, lastMFI, ok := alphaTrendSeriesCompact(highs, lows, closes, volumes, period, multiplier)
	if ok {
		return points, lastMFI, true
	}
	return alphaTrendSeriesBatch(highs, lows, closes, volumes, period, multiplier)
}

func alphaTrendSeriesCompact(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) ([]trendPoint, float64, bool) {
	if period <= 0 || len(closes) <= period || len(highs) != len(closes) || len(lows) != len(closes) || len(volumes) != len(closes) {
		return nil, 0, false
	}
	points := make([]trendPoint, 0, len(closes)-period)
	atrSum := 0.0
	positiveFlow := 0.0
	negativeFlow := 0.0
	for index := 1; index <= period; index++ {
		atrSum += trueRangeAt(highs, lows, closes, index)
		current := (highs[index] + lows[index] + closes[index]) / 3
		previous := (highs[index-1] + lows[index-1] + closes[index-1]) / 3
		flow := current * volumes[index]
		if current >= previous {
			positiveFlow += flow
		} else {
			negativeFlow += flow
		}
	}
	lastMFI := 50.0
	for index := period; index < len(closes); index++ {
		mfi := moneyFlowIndexFromSums(positiveFlow, negativeFlow)
		lastMFI = mfi
		atrValue := atrSum / float64(period)
		up := lows[index] - multiplier*atrValue
		down := highs[index] + multiplier*atrValue
		point := trendPoint{}
		if index == period {
			if mfi >= 50 {
				point = trendPoint{value: up, direction: trendDirectionUp}
			} else {
				point = trendPoint{value: down, direction: trendDirectionDown}
			}
		} else if mfi >= 50 {
			previous := points[len(points)-1]
			if up < previous.value {
				point = trendPoint{value: previous.value, direction: trendDirectionUp}
			} else {
				point = trendPoint{value: up, direction: trendDirectionUp}
			}
		} else {
			previous := points[len(points)-1]
			if down > previous.value {
				point = trendPoint{value: previous.value, direction: trendDirectionDown}
			} else {
				point = trendPoint{value: down, direction: trendDirectionDown}
			}
		}
		points = append(points, point)
		if index+1 < len(closes) {
			atrSum += trueRangeAt(highs, lows, closes, index+1) - trueRangeAt(highs, lows, closes, index-period+1)
			addCurrent := (highs[index+1] + lows[index+1] + closes[index+1]) / 3
			addPrevious := (highs[index] + lows[index] + closes[index]) / 3
			addFlow := addCurrent * volumes[index+1]
			if addCurrent >= addPrevious {
				positiveFlow += addFlow
			} else {
				negativeFlow += addFlow
			}
			dropCurrent := (highs[index-period+1] + lows[index-period+1] + closes[index-period+1]) / 3
			dropPrevious := (highs[index-period] + lows[index-period] + closes[index-period]) / 3
			dropFlow := dropCurrent * volumes[index-period+1]
			if dropCurrent >= dropPrevious {
				positiveFlow -= dropFlow
			} else {
				negativeFlow -= dropFlow
			}
		}
	}
	if len(points) < 2 {
		return nil, 0, false
	}
	return points, lastMFI, true
}

func trueRangeAt(highs []float64, lows []float64, closes []float64, index int) float64 {
	return maxFloat(
		highs[index]-lows[index],
		absFloat(highs[index]-closes[index-1]),
		absFloat(lows[index]-closes[index-1]),
	)
}

func moneyFlowIndexFromSums(positive float64, negative float64) float64 {
	if negative == 0 {
		return 100
	}
	ratio := positive / negative
	return 100 - 100/(1+ratio)
}

func alphaTrendSeriesBatch(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) ([]trendPoint, float64, bool) {
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
		points = append(points, trendPoint{value: trend[index], direction: trendDirectionFromString(directions[index])})
	}
	if len(points) < 2 {
		return nil, 0, false
	}
	return points, lastMFI, true
}

func alphaTrendSignals(points []trendPoint) (string, string) {
	if len(points) < 4 {
		return "none", "none"
	}
	last := len(points) - 1
	buy, sell := alphaTrendCrossAt(points, last)
	cross := "none"
	signal := "none"
	if buy {
		cross = "buy"
		if alphaTrendSignalAllowed(alphaTrendPreviousCrossDistance(points, last, true), alphaTrendCrossDistance(points, last, false)) {
			signal = "buy"
		}
	}
	if sell {
		cross = "sell"
		if alphaTrendSignalAllowed(alphaTrendPreviousCrossDistance(points, last, false), alphaTrendCrossDistance(points, last, true)) {
			signal = "sell"
		}
	}
	return cross, signal
}

func alphaTrendCrossAt(points []trendPoint, index int) (bool, bool) {
	if index < 3 || index >= len(points) {
		return false, false
	}
	current := points[index].value
	twoBack := points[index-2].value
	previous := points[index-1].value
	threeBack := points[index-3].value
	return current > twoBack && previous <= threeBack, current < twoBack && previous >= threeBack
}

func alphaTrendCrossDistance(points []trendPoint, current int, buy bool) int {
	for index := current; index >= 3; index-- {
		isBuy, isSell := alphaTrendCrossAt(points, index)
		if (buy && isBuy) || (!buy && isSell) {
			return current - index
		}
	}
	return -1
}

func alphaTrendPreviousCrossDistance(points []trendPoint, current int, buy bool) int {
	for index := current - 1; index >= 3; index-- {
		isBuy, isSell := alphaTrendCrossAt(points, index)
		if (buy && isBuy) || (!buy && isSell) {
			return current - index - 1
		}
	}
	return -1
}

func alphaTrendSignalAllowed(previousSame int, opposite int) bool {
	return previousSame >= 0 && opposite >= 0 && previousSame > opposite
}

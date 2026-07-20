package indicatorcalc

func addSupertrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64) {
	addSupertrendWithState(values, signals, highs, lows, closes, period, multiplier, nil)
}

func addSupertrendWithState(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64, basic *basicIndicatorState) {
	addSupertrendWithStateToSet(nil, values, signals, highs, lows, closes, period, multiplier, basic)
}

func addSupertrendWithStateToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64, basic *basicIndicatorState) {
	addSupertrendWithStateAndFeaturesToSet(target, values, signals, highs, lows, closes, period, multiplier, basic, nil)
}

func addSupertrendWithStateAndFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64, basic *basicIndicatorState, features *featureContext) {
	trueRanges, trueRangesOK := trueRangeSeries(highs, lows, closes), true
	if features != nil {
		trueRanges, trueRangesOK = features.trueRangeSeries()
	}
	if !trueRangesOK {
		return
	}
	points, ok := supertrendSeriesFromTrueRanges(highs, lows, closes, trueRanges, period, multiplier, false)
	if !ok {
		return
	}
	lastIndex := len(points) - 1
	lastPoint := points[lastIndex]
	lastClose := closes[len(closes)-1]
	setValueTarget(target, values, "supertrend", lastPoint.value, true)
	setValueTarget(target, values, "supertrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValueTarget(target, values, "supertrend_stop_distance_pct", absFloat(percentDistance(lastClose, lastPoint.value)), lastPoint.value != 0)
	signals["supertrend_direction"] = lastPoint.direction.String()
	signals["supertrend_flip"] = trendFlip(points[lastIndex-1].direction.String(), lastPoint.direction.String())

	smaPoints, smaOK := supertrendSeriesFromTrueRanges(highs, lows, closes, trueRanges, period, multiplier, true)
	if smaOK {
		smaLastIndex := len(smaPoints) - 1
		smaLastPoint := smaPoints[smaLastIndex]
		setValueTarget(target, values, "sma_atr_supertrend", smaLastPoint.value, true)
		setValueTarget(target, values, "sma_atr_supertrend_distance_pct", percentDistance(lastClose, smaLastPoint.value), smaLastPoint.value != 0)
		signals["sma_atr_supertrend_direction"] = smaLastPoint.direction.String()
		signals["sma_atr_supertrend_flip"] = trendFlip(smaPoints[smaLastIndex-1].direction.String(), smaLastPoint.direction.String())
	}

	for _, preset := range []struct {
		name       string
		period     int
		multiplier float64
	}{
		{name: "supertrend_7_2", period: 7, multiplier: 2},
		{name: "supertrend_10_3", period: 10, multiplier: 3},
		{name: "supertrend_10_3_3", period: 10, multiplier: 3.3},
		{name: "supertrend_14_4", period: 14, multiplier: 4},
	} {
		presetValue, presetDirection, presetOK := 0.0, "", false
		if preset.period == period && preset.multiplier == multiplier {
			presetValue, presetDirection, presetOK = lastPoint.value, lastPoint.direction.String(), true
		} else {
			presetValue, presetDirection, presetOK = supertrend(highs, lows, closes, preset.period, preset.multiplier)
		}
		if !presetOK {
			continue
		}
		setValueTarget(target, values, preset.name, presetValue, true)
		signals[preset.name+"_direction"] = presetDirection
	}

	addAdaptiveSupertrendWithStateToSet(target, values, signals, highs, lows, closes, 10, 3, 100, basic)
	var atr10 []float64
	var atr10OK bool
	if features != nil {
		atr10, atr10OK = features.atrSeries(10)
	}
	addAISupertrendWithATRToSet(target, values, signals, highs, lows, closes, 10, 1, 5, 0.5, 10, atr10, atr10OK)

	zone, zoneOK := supertrendZone(highs, lows, closes, points, period, 14, 1.5)
	if !zoneOK {
		signals["supertrend_zone_ready"] = "false"
		return
	}
	setValueTarget(target, values, "supertrend_zone_pivot_high", zone.pivotHigh, true)
	setValueTarget(target, values, "supertrend_zone_pivot_low", zone.pivotLow, true)
	setValueTarget(target, values, "supertrend_zone_mid", zone.mid, true)
	setValueTarget(target, values, "supertrend_zone_fib_236", zone.fib236, true)
	setValueTarget(target, values, "supertrend_zone_fib_382", zone.fib382, true)
	setValueTarget(target, values, "supertrend_zone_fib_5", zone.fib5, true)
	setValueTarget(target, values, "supertrend_zone_fib_618", zone.fib618, true)
	setValueTarget(target, values, "supertrend_zone_fib_786", zone.fib786, true)
	setValueTarget(target, values, "supertrend_zone_extension_1618", zone.extension, true)
	setValueTarget(target, values, "supertrend_zone_premium_band", zone.premiumBand, true)
	setValueTarget(target, values, "supertrend_zone_discount_band", zone.discountBand, true)
	setValueTarget(target, values, "supertrend_zone_position_pct", zone.positionPct, true)
	signals["supertrend_zone_side"] = zone.side
	signals["supertrend_zone_area"] = zone.area
	signals["supertrend_zone_ready"] = "true"
}

func supertrend(highs []float64, lows []float64, closes []float64, period int, multiplier float64) (float64, string, bool) {
	if period <= 0 || len(closes) <= period+1 || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, "", false
	}
	atrSum := 0.0
	for index := 1; index <= period; index++ {
		atrSum += maxFloat(
			highs[index]-lows[index],
			absFloat(highs[index]-closes[index-1]),
			absFloat(lows[index]-closes[index-1]),
		)
	}
	atrValue := atrSum / float64(period)
	mid := (highs[period] + lows[period]) / 2
	finalUpper := mid + multiplier*atrValue
	finalLower := mid - multiplier*atrValue
	direction := "down"
	if closes[period] >= mid {
		direction = "up"
	}
	for index := period + 1; index < len(closes); index++ {
		trueRange := maxFloat(
			highs[index]-lows[index],
			absFloat(highs[index]-closes[index-1]),
			absFloat(lows[index]-closes[index-1]),
		)
		atrValue = (atrValue*float64(period-1) + trueRange) / float64(period)
		mid = (highs[index] + lows[index]) / 2
		basicUpper := mid + multiplier*atrValue
		basicLower := mid - multiplier*atrValue
		nextUpper := finalUpper
		if basicUpper < finalUpper || closes[index-1] > finalUpper {
			nextUpper = basicUpper
		}
		nextLower := finalLower
		if basicLower > finalLower || closes[index-1] < finalLower {
			nextLower = basicLower
		}
		if direction == "down" && closes[index] > nextUpper {
			direction = "up"
		} else if direction == "up" && closes[index] < nextLower {
			direction = "down"
		}
		finalUpper, finalLower = nextUpper, nextLower
	}
	return supertrendPoint(finalUpper, finalLower, direction).value, direction, true
}

type trendDirection uint8

const (
	trendDirectionUnknown trendDirection = iota
	trendDirectionDown
	trendDirectionUp
)

func (d trendDirection) String() string {
	switch d {
	case trendDirectionDown:
		return "down"
	case trendDirectionUp:
		return "up"
	default:
		return ""
	}
}

func trendDirectionFromString(direction string) trendDirection {
	switch direction {
	case "down":
		return trendDirectionDown
	case "up":
		return trendDirectionUp
	default:
		return trendDirectionUnknown
	}
}

type trendPoint struct {
	value     float64
	direction trendDirection
}

func supertrendSeriesWithATR(highs []float64, lows []float64, closes []float64, atrValues []float64, start int, multiplier float64) ([]trendPoint, bool) {
	if start <= 0 || start >= len(closes) || len(atrValues) != len(closes) {
		return nil, false
	}
	if atrValues[start] <= 0 {
		return nil, false
	}
	points := make([]trendPoint, 0, len(closes)-start)
	mid := (highs[start] + lows[start]) / 2
	finalUpper := mid + multiplier*atrValues[start]
	finalLower := mid - multiplier*atrValues[start]
	direction := "down"
	if closes[start] >= mid {
		direction = "up"
	}
	points = append(points, supertrendPoint(finalUpper, finalLower, direction))
	for index := start + 1; index < len(closes); index++ {
		if atrValues[index] <= 0 {
			return nil, false
		}
		mid = (highs[index] + lows[index]) / 2
		basicUpper := mid + multiplier*atrValues[index]
		basicLower := mid - multiplier*atrValues[index]
		nextUpper := finalUpper
		if basicUpper < finalUpper || closes[index-1] > finalUpper {
			nextUpper = basicUpper
		}
		nextLower := finalLower
		if basicLower > finalLower || closes[index-1] < finalLower {
			nextLower = basicLower
		}
		nextDirection := direction
		if direction == "down" && closes[index] > nextUpper {
			nextDirection = "up"
		} else if direction == "up" && closes[index] < nextLower {
			nextDirection = "down"
		}
		finalUpper, finalLower, direction = nextUpper, nextLower, nextDirection
		points = append(points, supertrendPoint(finalUpper, finalLower, direction))
	}
	if len(points) < 2 {
		return nil, false
	}
	return points, true
}

func supertrendSeries(highs []float64, lows []float64, closes []float64, period int, multiplier float64) ([]trendPoint, bool) {
	if len(highs) != len(closes) || len(lows) != len(closes) {
		return nil, false
	}
	return supertrendSeriesFromTrueRanges(highs, lows, closes, trueRangeSeries(highs, lows, closes), period, multiplier, false)
}

// supertrendSeriesSMAATR implements the Pine variant that uses sma(true range,
// period) instead of Wilder's ATR. Band continuation and flip semantics remain
// identical to the standard Supertrend implementation.
func supertrendSeriesSMAATR(highs []float64, lows []float64, closes []float64, period int, multiplier float64) ([]trendPoint, bool) {
	if len(highs) != len(closes) || len(lows) != len(closes) {
		return nil, false
	}
	return supertrendSeriesFromTrueRanges(highs, lows, closes, trueRangeSeries(highs, lows, closes), period, multiplier, true)
}

func supertrendSeriesFromTrueRanges(highs []float64, lows []float64, closes []float64, trueRanges []float64, period int, multiplier float64, simpleATR bool) ([]trendPoint, bool) {
	if period <= 0 || len(closes) <= period || len(highs) != len(closes) || len(lows) != len(closes) || len(trueRanges) != len(closes) {
		return nil, false
	}
	if simpleATR && multiplier <= 0 {
		return nil, false
	}
	atrSum := 0.0
	for index := 1; index <= period; index++ {
		atrSum += trueRanges[index]
	}
	atrValue := atrSum / float64(period)
	mid := (highs[period] + lows[period]) / 2
	finalUpper := mid + multiplier*atrValue
	finalLower := mid - multiplier*atrValue
	direction := "down"
	if closes[period] >= mid {
		direction = "up"
	}
	points := make([]trendPoint, 0, len(closes)-period)
	points = append(points, supertrendPoint(finalUpper, finalLower, direction))
	for index := period + 1; index < len(closes); index++ {
		if simpleATR {
			atrSum += trueRanges[index] - trueRanges[index-period]
			atrValue = atrSum / float64(period)
		} else {
			atrValue = (atrValue*float64(period-1) + trueRanges[index]) / float64(period)
		}
		mid = (highs[index] + lows[index]) / 2
		basicUpper := mid + multiplier*atrValue
		basicLower := mid - multiplier*atrValue
		nextUpper := finalUpper
		if basicUpper < finalUpper || closes[index-1] > finalUpper {
			nextUpper = basicUpper
		}
		nextLower := finalLower
		if basicLower > finalLower || closes[index-1] < finalLower {
			nextLower = basicLower
		}
		if direction == "down" && closes[index] > nextUpper {
			direction = "up"
		} else if direction == "up" && closes[index] < nextLower {
			direction = "down"
		}
		finalUpper, finalLower = nextUpper, nextLower
		points = append(points, supertrendPoint(finalUpper, finalLower, direction))
	}
	if len(points) < 2 {
		return nil, false
	}
	return points, true
}

func supertrendPoint(finalUpper float64, finalLower float64, direction string) trendPoint {
	if direction == "down" {
		return trendPoint{value: finalUpper, direction: trendDirectionDown}
	}
	return trendPoint{value: finalLower, direction: trendDirectionFromString(direction)}
}

package indicatorcalc

type supertrendZoneState struct {
	pivotHigh    float64
	pivotLow     float64
	mid          float64
	fib236       float64
	fib382       float64
	fib5         float64
	fib618       float64
	fib786       float64
	extension    float64
	premiumBand  float64
	discountBand float64
	positionPct  float64
	side         string
	area         string
}

func supertrendZone(highs []float64, lows []float64, closes []float64, points []trendPoint, supertrendPeriod int, atrPeriod int, atrMultiplier float64) (supertrendZoneState, bool) {
	return supertrendZoneWithATR(highs, lows, closes, points, supertrendPeriod, atrPeriod, atrMultiplier, 0, false)
}

func supertrendZoneWithATR(highs []float64, lows []float64, closes []float64, points []trendPoint, supertrendPeriod int, atrPeriod int, atrMultiplier float64, atrValue float64, atrOK bool) (supertrendZoneState, bool) {
	if len(points) < 2 || len(highs) != len(closes) || len(lows) != len(closes) || supertrendPeriod <= 0 {
		return supertrendZoneState{}, false
	}
	offset := len(closes) - len(points)
	if offset < 0 || offset >= len(closes) {
		return supertrendZoneState{}, false
	}
	var pivotHigh float64
	var pivotLow float64
	hasPivotHigh := false
	hasPivotLow := false
	segmentStart := offset
	previousDirection := points[0].direction
	for pointIndex := 1; pointIndex < len(points); pointIndex++ {
		currentDirection := points[pointIndex].direction
		if currentDirection == previousDirection {
			continue
		}
		seriesIndex := pointIndex + offset
		highest, lowest := highLow(highs[segmentStart:seriesIndex+1], lows[segmentStart:seriesIndex+1])
		if previousDirection == trendDirectionUp && currentDirection == trendDirectionDown {
			pivotHigh = highest
			hasPivotHigh = true
		}
		if previousDirection == trendDirectionDown && currentDirection == trendDirectionUp {
			pivotLow = lowest
			hasPivotLow = true
		}
		segmentStart = seriesIndex
		previousDirection = currentDirection
	}
	if !hasPivotHigh || !hasPivotLow || pivotHigh == pivotLow {
		return supertrendZoneState{}, false
	}
	if pivotHigh < pivotLow {
		pivotHigh, pivotLow = pivotLow, pivotHigh
	}
	if !atrOK {
		var ok bool
		atrValue, ok = atr(highs, lows, closes, atrPeriod)
		if !ok {
			return supertrendZoneState{}, false
		}
	}
	lastPoint := points[len(points)-1]
	lastClose := closes[len(closes)-1]
	priceRange := pivotHigh - pivotLow
	positionPct := (lastClose - pivotLow) / priceRange * 100
	zone := supertrendZoneState{
		pivotHigh:    pivotHigh,
		pivotLow:     pivotLow,
		mid:          pivotLow + priceRange*0.5,
		fib236:       pivotLow + priceRange*0.236,
		fib382:       pivotLow + priceRange*0.382,
		fib5:         pivotLow + priceRange*0.5,
		fib618:       pivotLow + priceRange*0.618,
		fib786:       pivotLow + priceRange*0.786,
		premiumBand:  lastPoint.value + atrValue*atrMultiplier,
		discountBand: lastPoint.value - atrValue*atrMultiplier,
		positionPct:  positionPct,
		side:         supertrendZoneSide(lastPoint.direction),
		area:         supertrendZoneArea(positionPct),
	}
	if lastPoint.direction == trendDirectionDown {
		zone.extension = pivotLow - priceRange*0.618
	} else {
		zone.extension = pivotLow + priceRange*1.618
	}
	return zone, true
}

func supertrendZoneSide(direction trendDirection) string {
	if direction == trendDirectionDown {
		return "bear"
	}
	return "bull"
}

func supertrendZoneArea(positionPct float64) string {
	switch {
	case positionPct < 0 || positionPct > 100:
		return "extension"
	case positionPct < 38.2:
		return "discount"
	case positionPct <= 61.8:
		return "mid"
	default:
		return "premium"
	}
}

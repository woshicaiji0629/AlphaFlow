package indicatorcalc

import "math"

func addFibonacciFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	levels, ok := fibonacciLevels(highs, lows, closes, 50)
	if !ok {
		return
	}
	setValue(values, "fib_236", levels[0], true)
	setValue(values, "fib_382", levels[1], true)
	setValue(values, "fib_5", levels[2], true)
	setValue(values, "fib_618", levels[3], true)
	setValue(values, "fib_786", levels[4], true)
	signals["fib_zone"] = fibonacciZone(closes[len(closes)-1], levels)
}

func fibonacciLevels(highs []float64, lows []float64, closes []float64, lookback int) ([5]float64, bool) {
	levels := [5]float64{}
	if len(closes) < 8 {
		return levels, false
	}
	period := minInt(lookback, len(closes))
	start := len(closes) - period
	pivotHighs, pivotLows := pivots(highs[start:], lows[start:], 2)
	var swingHigh priceLevel
	var swingLow priceLevel
	var okHigh bool
	var okLow bool
	if len(pivotHighs) > 0 {
		swingHigh, okHigh = recentSwing(pivotHighs)
	}
	if len(pivotLows) > 0 {
		swingLow, okLow = recentSwing(pivotLows)
	}
	if !okHigh || !okLow {
		high, low := highLow(highs[start:], lows[start:])
		swingHigh = priceLevel{price: high}
		swingLow = priceLevel{price: low}
	}
	if swingHigh.price == swingLow.price {
		return levels, false
	}
	high := swingHigh.price
	low := swingLow.price
	if swingLow.recency > swingHigh.recency {
		levels = retracementLevels(low, high, true)
	} else {
		levels = retracementLevels(low, high, false)
	}
	return levels, true
}

func retracementLevels(low float64, high float64, upMove bool) [5]float64 {
	diff := high - low
	if upMove {
		return [5]float64{
			low + diff*0.236,
			low + diff*0.382,
			low + diff*0.5,
			low + diff*0.618,
			low + diff*0.786,
		}
	}
	return [5]float64{
		high - diff*0.236,
		high - diff*0.382,
		high - diff*0.5,
		high - diff*0.618,
		high - diff*0.786,
	}
}

func fibonacciZone(price float64, levels [5]float64) string {
	names := [5]string{"near_236", "near_382", "near_5", "near_618", "near_786"}
	bestIndex := 0
	bestDistance := math.Abs(price - levels[0])
	for index := 1; index < len(levels); index++ {
		distance := math.Abs(price - levels[index])
		if distance < bestDistance {
			bestIndex = index
			bestDistance = distance
		}
	}
	tolerance := math.Max(math.Abs(price)*0.002, 0.00000001)
	if bestDistance <= tolerance {
		return names[bestIndex]
	}
	return "none"
}

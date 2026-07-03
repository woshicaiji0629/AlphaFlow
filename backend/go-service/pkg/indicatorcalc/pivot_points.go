package indicatorcalc

import "math"

func addPivotPointFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	pivot, r1, r2, s1, s2, ok := pivotPoints(highs, lows, closes, 20)
	if !ok {
		return
	}
	setValue(values, "pivot_point", pivot, true)
	setValue(values, "pivot_r1", r1, true)
	setValue(values, "pivot_r2", r2, true)
	setValue(values, "pivot_s1", s1, true)
	setValue(values, "pivot_s2", s2, true)
	signals["pivot_zone"] = pivotZone(closes[len(closes)-1], pivot)
}

func pivotPoints(highs []float64, lows []float64, closes []float64, period int) (float64, float64, float64, float64, float64, bool) {
	if period <= 0 || len(closes) < period {
		return 0, 0, 0, 0, 0, false
	}
	start := len(closes) - period
	high, low := highLow(highs[start:], lows[start:])
	closeValue := closes[len(closes)-1]
	pivot := (high + low + closeValue) / 3
	r1 := 2*pivot - low
	s1 := 2*pivot - high
	r2 := pivot + (high - low)
	s2 := pivot - (high - low)
	return pivot, r1, r2, s1, s2, true
}

func pivotZone(price float64, pivot float64) string {
	if pivot == 0 {
		return "near_pivot"
	}
	if math.Abs(price-pivot) <= math.Abs(pivot)*0.002 {
		return "near_pivot"
	}
	if price > pivot {
		return "above_pivot"
	}
	return "below_pivot"
}

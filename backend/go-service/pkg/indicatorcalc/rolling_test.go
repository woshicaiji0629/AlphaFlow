package indicatorcalc

import (
	"math"
	"testing"
)

func TestRollingWindowMatchesFullRecalculation(t *testing.T) {
	rolling := newRollingWindow(20)
	values := make([]float64, 0, 80)
	for index := 0; index < 80; index++ {
		value := 10000 + math.Sin(float64(index))*3 + float64(index%7)/10
		values = append(values, value)
		rolling.append(value)
		if len(values) < 20 {
			continue
		}
		window := values[len(values)-20:]
		mean, variance, ok := rolling.meanVariance()
		if !ok {
			t.Fatalf("index %d: rolling variance not ready", index)
		}
		wantMean, _ := sma(window, 20)
		var wantVariance float64
		for _, item := range window {
			delta := item - wantMean
			wantVariance += delta * delta
		}
		wantVariance /= 20
		if math.Abs(mean-wantMean) > 1e-10 || math.Abs(variance-wantVariance) > 2e-7 {
			t.Fatalf("index %d: mean/variance = %.12f/%.12f, want %.12f/%.12f", index, mean, variance, wantMean, wantVariance)
		}
		gotHigh, gotLow, ok := rolling.rangeValues()
		if !ok {
			t.Fatalf("index %d: rolling range not ready", index)
		}
		wantHigh, wantLow := highLow(window, window)
		if gotHigh != wantHigh || gotLow != wantLow {
			t.Fatalf("index %d: range = %v/%v, want %v/%v", index, gotHigh, gotLow, wantHigh, wantLow)
		}
	}
}

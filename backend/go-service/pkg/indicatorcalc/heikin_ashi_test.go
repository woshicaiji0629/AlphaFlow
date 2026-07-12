package indicatorcalc

import "testing"

func TestHeikinAshiFeatures(t *testing.T) {
	opens := []float64{10, 11, 12, 13}
	highs := []float64{12, 13, 14, 15}
	lows := []float64{9, 10, 11, 12}
	closes := []float64{11, 12, 13, 14}
	values := map[string]string{}
	signals := map[string]string{}

	addHeikinAshiFeatures(values, signals, opens, highs, lows, closes)

	for _, key := range []string{"ha_open", "ha_high", "ha_low", "ha_close"} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["ha_trend"] != "bull" {
		t.Fatalf("ha_trend = %q, want bull", signals["ha_trend"])
	}
	if signals["ha_strength"] == "" {
		t.Fatalf("missing ha_strength: %#v", signals)
	}
}

func TestHeikinAshiSeriesRejectsMismatchedInput(t *testing.T) {
	_, ok := heikinAshiSeries([]float64{1}, []float64{1, 2}, []float64{1}, []float64{1})
	if ok {
		t.Fatal("heikinAshiSeries returned true for mismatched input")
	}
}

func TestHeikinAshiLastMatchesSeries(t *testing.T) {
	opens := []float64{100, 102, 101, 105}
	highs := []float64{103, 104, 106, 108}
	lows := []float64{99, 100, 100, 103}
	closes := []float64{102, 101, 105, 107}
	series, ok := heikinAshiSeries(opens, highs, lows, closes)
	if !ok {
		t.Fatal("heikinAshiSeries returned false")
	}
	last, ok := heikinAshiLast(opens, highs, lows, closes)
	if !ok {
		t.Fatal("heikinAshiLast returned false")
	}
	if last != series[len(series)-1] {
		t.Fatalf("last = %#v, want %#v", last, series[len(series)-1])
	}
}

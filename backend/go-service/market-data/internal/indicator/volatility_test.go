package indicator

import "testing"

func TestSupertrendUsesSeriesDirection(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(80, 100, 0.8)
	values := map[string]string{}
	signals := map[string]string{}

	addSupertrend(values, signals, highs, lows, closes, 10, 3)
	addAlphaTrend(values, signals, highs, lows, closes, volumes, 14, 1)

	if values["supertrend"] == "" {
		t.Fatalf("missing supertrend: %#v", values)
	}
	if signals["supertrend_direction"] != "up" {
		t.Fatalf("supertrend_direction = %q, want up", signals["supertrend_direction"])
	}
	if values["alphatrend"] == "" || values["mfi14"] == "" {
		t.Fatalf("missing alphatrend values: %#v", values)
	}
	if signals["alphatrend_direction"] == "" {
		t.Fatalf("missing alphatrend direction: %#v", signals)
	}
}

func TestSqueezeMomentumOutputsDelta(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 0.35)
	values := map[string]string{}
	signals := map[string]string{}

	addSqueezeMomentum(values, signals, highs, lows, closes)

	if values["squeeze_momentum"] == "" {
		t.Fatalf("missing squeeze_momentum: %#v", values)
	}
	if values["squeeze_momentum_delta"] == "" {
		t.Fatalf("missing squeeze_momentum_delta: %#v", values)
	}
	if signals["squeeze"] == "" || signals["momentum_state"] == "" {
		t.Fatalf("missing squeeze signals: %#v", signals)
	}
}

func TestSqueezeMomentumAtUsesRangeBaseline(t *testing.T) {
	highs := []float64{11, 12, 13, 14, 15}
	lows := []float64{9, 10, 11, 12, 13}
	closes := []float64{10, 11, 12, 13, 14}

	value, ok := squeezeMomentumAt(highs, lows, closes, 5, 5)
	if !ok {
		t.Fatal("squeezeMomentumAt returned false")
	}
	if value != 2 {
		t.Fatalf("momentum = %v, want 2", value)
	}
}

func trendingSeries(length int, start float64, step float64) ([]float64, []float64, []float64, []float64) {
	highs := make([]float64, 0, length)
	lows := make([]float64, 0, length)
	closes := make([]float64, 0, length)
	volumes := make([]float64, 0, length)
	for index := 0; index < length; index++ {
		closeValue := start + float64(index)*step
		highs = append(highs, closeValue+1.2)
		lows = append(lows, closeValue-1)
		closes = append(closes, closeValue)
		volumes = append(volumes, 100+float64(index%7))
	}
	return highs, lows, closes, volumes
}

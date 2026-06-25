package indicator

import "testing"

func TestSupertrendUsesSeriesDirection(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(80, 100, 0.8)
	values := map[string]string{}
	signals := map[string]string{}

	addSupertrend(values, signals, highs, lows, closes, 10, 3)
	addAlphaTrend(values, signals, highs, lows, closes, volumes, 14, 1)
	addPSARFeatures(values, signals, highs, lows, closes)

	if values["supertrend"] == "" {
		t.Fatalf("missing supertrend: %#v", values)
	}
	for _, key := range []string{
		"supertrend_distance_pct",
		"supertrend_stop_distance_pct",
		"supertrend_7_2",
		"supertrend_10_3",
		"supertrend_14_4",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s: %#v", key, values)
		}
	}
	if signals["supertrend_direction"] != "up" {
		t.Fatalf("supertrend_direction = %q, want up", signals["supertrend_direction"])
	}
	if signals["supertrend_flip"] == "" {
		t.Fatalf("missing supertrend flip: %#v", signals)
	}
	if signals["supertrend_7_2_direction"] == "" || signals["supertrend_10_3_direction"] == "" || signals["supertrend_14_4_direction"] == "" {
		t.Fatalf("missing supertrend preset directions: %#v", signals)
	}
	if values["alphatrend"] == "" || values["mfi14"] == "" {
		t.Fatalf("missing alphatrend values: %#v", values)
	}
	if values["alphatrend_distance_pct"] == "" || values["alphatrend_slope_pct"] == "" {
		t.Fatalf("missing alphatrend distance/slope: %#v", values)
	}
	if signals["alphatrend_direction"] == "" {
		t.Fatalf("missing alphatrend direction: %#v", signals)
	}
	if signals["alphatrend_flip"] == "" {
		t.Fatalf("missing alphatrend flip: %#v", signals)
	}
	if values["psar"] == "" || values["psar_distance_pct"] == "" {
		t.Fatalf("missing psar values: %#v", values)
	}
	if signals["psar_direction"] != "up" {
		t.Fatalf("psar_direction = %q, want up", signals["psar_direction"])
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

func TestBollingerShapeFeatures(t *testing.T) {
	_, _, closes, _ := trendingSeries(80, 100, 0.35)
	values := map[string]string{}
	signals := map[string]string{}

	addBollingerFeatures(values, signals, closes)

	for _, key := range []string{
		"bb_width_pct",
		"bb_percent_b",
		"bb_width_delta",
		"bb_middle_slope_pct",
		"bb_upper_slope_pct",
		"bb_lower_slope_pct",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["bb_width_state"] == "" || signals["bb_trend"] == "" {
		t.Fatalf("missing bollinger signals: %#v", signals)
	}
}

func TestBollingerStateHelpers(t *testing.T) {
	if got := bollingerWidthState(2, 10); got != "expanding" {
		t.Fatalf("bollingerWidthState expanding = %q", got)
	}
	if got := bollingerWidthState(-2, 10); got != "contracting" {
		t.Fatalf("bollingerWidthState contracting = %q", got)
	}
	if got := bollingerTrend(0.01); got != "flat" {
		t.Fatalf("bollingerTrend flat = %q", got)
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

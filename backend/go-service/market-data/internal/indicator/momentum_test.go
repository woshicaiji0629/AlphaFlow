package indicator

import "testing"

func TestRSIUsesWilderSmoothing(t *testing.T) {
	values := linearValues(60, 100, 1)

	got, ok := rsi(values, 14)
	if !ok {
		t.Fatal("rsi returned false")
	}
	if got != 100 {
		t.Fatalf("rsi = %v, want 100", got)
	}
}

func TestRSIFeatures(t *testing.T) {
	values := map[string]string{}
	signals := map[string]string{}

	addRSIFeatures(values, signals, linearValues(60, 100, 1), 14)

	if values["rsi14"] == "" || values["rsi_slope3"] == "" {
		t.Fatalf("missing rsi values: %#v", values)
	}
	if signals["rsi_state"] != "overbought" {
		t.Fatalf("rsi_state = %q, want overbought", signals["rsi_state"])
	}
}

func TestOscillatorFeatures(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 0.8)
	values := map[string]string{}
	signals := map[string]string{}

	addOscillatorFeatures(values, signals, highs, lows, closes)

	for _, key := range []string{
		"kdj_k",
		"stoch_k",
		"stoch_rsi_k",
		"skdj_k",
		"cci20",
		"williams_r14",
		"roc12",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	for _, key := range []string{
		"stoch_rsi_state",
		"skdj_cross",
		"cci_state",
		"williams_state",
		"roc_state",
	} {
		if signals[key] == "" {
			t.Fatalf("missing %s in %#v", key, signals)
		}
	}
}

func TestWilliamsRAndROC(t *testing.T) {
	highs := []float64{10, 11, 12, 13, 14}
	lows := []float64{8, 9, 10, 11, 12}
	closes := []float64{9, 10, 11, 12, 13}

	williams, ok := williamsR(highs, lows, closes, 5)
	if !ok {
		t.Fatal("williamsR returned false")
	}
	if williams >= 0 || williams < -100 {
		t.Fatalf("williamsR = %v, want [-100, 0)", williams)
	}
	rocValue, ok := roc(closes, 2)
	if !ok {
		t.Fatal("roc returned false")
	}
	if rocValue <= 0 {
		t.Fatalf("roc = %v, want positive", rocValue)
	}
}

func TestCrossSignal(t *testing.T) {
	if got := crossSignal(10, 20, 30, 20); got != "golden" {
		t.Fatalf("crossSignal golden = %q", got)
	}
	if got := crossSignal(30, 20, 10, 20); got != "dead" {
		t.Fatalf("crossSignal dead = %q", got)
	}
}

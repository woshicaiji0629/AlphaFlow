package indicator

import "testing"

func TestMovingAverageFeatures(t *testing.T) {
	closes := linearValues(120, 100, 0.5)
	volumes := linearValues(120, 10, 0.1)
	values := map[string]string{}
	signals := map[string]string{}

	addMovingAverageFeatures(values, signals, closes, volumes)

	for _, key := range []string{
		"hma21",
		"vwma20",
		"dema21",
		"tema21",
		"kama10",
		"hma21_slope3_pct",
		"ema_spread_pct",
		"ma_trend_strength",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["ma_state"] != "bull" {
		t.Fatalf("ma_state = %q, want bull", signals["ma_state"])
	}
}

func TestVWMAWeightsVolume(t *testing.T) {
	values := []float64{10, 20, 30}
	volumes := []float64{1, 1, 8}

	got, ok := vwma(values, volumes, 3)
	if !ok {
		t.Fatal("vwma returned false")
	}
	if got != 27 {
		t.Fatalf("vwma = %v, want 27", got)
	}
}

func TestKAMAHandlesFlatSeries(t *testing.T) {
	values := linearValues(40, 100, 0)

	got, ok := kama(values, 10, 2, 30)
	if !ok {
		t.Fatal("kama returned false")
	}
	if got != 100 {
		t.Fatalf("kama = %v, want 100", got)
	}
}

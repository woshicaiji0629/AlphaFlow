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
		"alligator_jaw",
		"alligator_teeth",
		"alligator_lips",
		"alligator_spread_pct",
		"ma_group_spread_pct",
		"hma21_slope3_pct",
		"ema_spread_pct",
		"ma_trend_strength",
		"script_dual_ma_out1",
		"script_dual_ma_out2",
		"script_dual_ma_out1_slope_pct",
		"script_dual_ma_out2_slope_pct",
		"script_ma_breakout_pct",
		"script_ma_mid_direction",
		"emd_avg",
		"emd_value",
		"emd_upper",
		"emd_lower",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["ma_state"] != "bull" {
		t.Fatalf("ma_state = %q, want bull", signals["ma_state"])
	}
	if signals["alligator_direction"] != "bull" {
		t.Fatalf("alligator_direction = %q, want bull", signals["alligator_direction"])
	}
	if signals["alligator_state"] == "" {
		t.Fatalf("missing alligator_state: %#v", signals)
	}
	for _, key := range []string{"ma_arrangement", "ma_cross", "ma_spread_state", "ma_compression", "ma_slope_state", "ma_breakout", "script_dual_ma_cross", "script_ma1_direction", "script_price_cross_ma1", "script_price_cross_ma2", "script_ma_signal", "emd_direction", "emd_cross"} {
		if signals[key] == "" {
			t.Fatalf("missing %s in %#v", key, signals)
		}
	}
}

func TestTilsonT3ReturnsValue(t *testing.T) {
	got, ok := tilsonT3(linearValues(120, 100, 0.5), 20, 0.7)
	if !ok {
		t.Fatal("tilsonT3 returned false")
	}
	if got <= 0 {
		t.Fatalf("tilsonT3 = %v, want positive", got)
	}
}

func TestMovingAverageByTypeUsesConfiguredAverage(t *testing.T) {
	values := linearValues(80, 100, 1)
	volumes := linearValues(80, 10, 1)

	got, ok := movingAverageByType(values, volumes, 20, 1, 0.7)
	if !ok {
		t.Fatal("movingAverageByType returned false")
	}
	want, _ := sma(values, 20)
	if got != want {
		t.Fatalf("movingAverageByType sma = %v, want %v", got, want)
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

func TestAlligatorHelpers(t *testing.T) {
	jaw, teeth, lips, ok := alligator(linearValues(80, 100, 1))
	if !ok {
		t.Fatal("alligator returned false")
	}
	if got := alligatorDirection(jaw, teeth, lips); got != "bull" {
		t.Fatalf("alligatorDirection = %q, want bull", got)
	}
	if got := alligatorState(0.1); got != "sleeping" {
		t.Fatalf("alligatorState sleeping = %q", got)
	}
	if got := alligatorState(1); got != "eating" {
		t.Fatalf("alligatorState eating = %q", got)
	}
}

func TestMovingAverageStructureHelpers(t *testing.T) {
	if got := movingAverageArrangement(3, 2, 1); got != "bull" {
		t.Fatalf("movingAverageArrangement bull = %q", got)
	}
	if got := spreadState(12, 10); got != "expanding" {
		t.Fatalf("spreadState expanding = %q", got)
	}
	if got := compressionState(0.1, 100); got != "compressed" {
		t.Fatalf("compressionState compressed = %q", got)
	}
	if got := movingAverageBreakout(110, 100, 101, 102); got != "above_group" {
		t.Fatalf("movingAverageBreakout = %q", got)
	}
}

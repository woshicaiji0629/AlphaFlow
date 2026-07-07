package indicatorcalc

import "testing"

func TestMovingAverageFeatures(t *testing.T) {
	closes := linearValues(240, 100, 0.5)
	volumes := linearValues(240, 10, 0.1)
	values := map[string]string{}
	signals := map[string]string{}

	addMovingAverageFeatures(values, signals, closes, volumes, nil)

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
		"ez_ema_5",
		"ez_ema_8",
		"ez_ema_9",
		"ez_ema_34",
		"ez_ema_55",
		"ez_ema_89",
		"ez_ema_144",
		"ez_ema_200",
		"ez_ema_fast",
		"ez_ema_slow",
		"ez_ema_spread_pct",
		"ez_ema_group_spread_pct",
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
	for _, key := range []string{"ma_arrangement", "ma_cross", "ma_spread_state", "ma_compression", "ma_slope_state", "ma_breakout", "ez_ema_cross", "ez_price_cross_ema_pair", "ez_price_above_ema_pair", "ez_price_below_ema_pair", "ez_ema_stack", "ez_ema_spread_state", "ez_ema_compression", "script_dual_ma_cross", "script_ma1_direction", "script_price_cross_ma1", "script_price_cross_ma2", "script_ma_signal", "emd_direction", "emd_cross"} {
		if signals[key] == "" {
			t.Fatalf("missing %s in %#v", key, signals)
		}
	}
	if signals["ez_ema_stack"] != "bull" {
		t.Fatalf("ez_ema_stack = %q, want bull", signals["ez_ema_stack"])
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

func TestHMAOptimizedMatchesFullDifferenceWindow(t *testing.T) {
	values := linearValues(240, 100, 0.5)

	got, ok := hma(values, 21)
	if !ok {
		t.Fatal("hma returned false")
	}
	want, ok := hmaFullDifferenceWindow(values, 21)
	if !ok {
		t.Fatal("hmaFullDifferenceWindow returned false")
	}
	assertFloatClose(t, "hma", got, want)
}

func TestDEMATEMAStreamedFinalValueMatchesSeries(t *testing.T) {
	values := linearValues(240, 100, 0.5)

	gotDEMA, ok := dema(values, 21)
	if !ok {
		t.Fatal("dema returned false")
	}
	wantDEMA, ok := demaFromSeries(values, 21)
	if !ok {
		t.Fatal("demaFromSeries returned false")
	}
	assertFloatClose(t, "dema", gotDEMA, wantDEMA)

	gotTEMA, ok := tema(values, 21)
	if !ok {
		t.Fatal("tema returned false")
	}
	wantTEMA, ok := temaFromSeries(values, 21)
	if !ok {
		t.Fatal("temaFromSeries returned false")
	}
	assertFloatClose(t, "tema", gotTEMA, wantTEMA)
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

func hmaFullDifferenceWindow(values []float64, period int) (float64, bool) {
	if period <= 1 || len(values) < period {
		return 0, false
	}
	half := period / 2
	sqrtPeriod := intSqrt(period)
	if sqrtPeriod < 1 {
		return 0, false
	}
	differences := make([]float64, 0, len(values)-period+1)
	for end := period; end <= len(values); end++ {
		halfWMA, okHalf := wma(values[end-half:end], half)
		fullWMA, okFull := wma(values[end-period:end], period)
		if !okHalf || !okFull {
			return 0, false
		}
		differences = append(differences, 2*halfWMA-fullWMA)
	}
	return wma(differences, sqrtPeriod)
}

func demaFromSeries(values []float64, period int) (float64, bool) {
	ema1, ok := emaSeries(values, period)
	if !ok {
		return 0, false
	}
	ema2, ok := emaSeries(ema1, period)
	if !ok {
		return 0, false
	}
	return 2*ema1[len(ema1)-1] - ema2[len(ema2)-1], true
}

func temaFromSeries(values []float64, period int) (float64, bool) {
	ema1, ok := emaSeries(values, period)
	if !ok {
		return 0, false
	}
	ema2, ok := emaSeries(ema1, period)
	if !ok {
		return 0, false
	}
	ema3, ok := emaSeries(ema2, period)
	if !ok {
		return 0, false
	}
	return 3*ema1[len(ema1)-1] - 3*ema2[len(ema2)-1] + ema3[len(ema3)-1], true
}

func intSqrt(value int) int {
	result := 0
	for (result+1)*(result+1) <= value {
		result++
	}
	return result
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

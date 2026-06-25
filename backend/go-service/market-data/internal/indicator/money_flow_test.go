package indicator

import "testing"

func TestMoneyFlowFeaturesConfirmUp(t *testing.T) {
	highs, lows, closes, volumes := moneyFlowSeries(30, 100, 1, 100)
	values := map[string]string{}
	signals := map[string]string{}

	addMoneyFlowFeatures(values, signals, highs, lows, closes, volumes)

	for _, key := range []string{
		"mfi14",
		"vwap_distance_pct",
		"rolling_vwap20",
		"rolling_vwap_distance_pct",
		"obv_slope5",
		"volume_zscore20",
		"volume_ratio5",
		"volume_ratio10",
		"volume_breakout_ratio",
		"volume_trend5",
		"volume_divergence_score",
		"volume_pressure20",
		"price_volume_trend",
		"cmf20",
		"ad_line",
		"ad_line_slope5",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["money_flow"] != "inflow" {
		t.Fatalf("money_flow = %q, want inflow", signals["money_flow"])
	}
	if signals["price_volume_confirmation"] != "confirm_up" {
		t.Fatalf("price_volume_confirmation = %q, want confirm_up", signals["price_volume_confirmation"])
	}
	if signals["cmf_state"] == "" {
		t.Fatalf("missing cmf_state: %#v", signals)
	}
	if signals["price_volume_action"] == "" || signals["breakout_volume_confirm"] == "" || signals["breakout_volume_strength"] == "" || signals["volume_divergence"] == "" || signals["volume_phase"] == "" {
		t.Fatalf("missing volume action signals: %#v", signals)
	}
}

func TestRollingVWAP(t *testing.T) {
	highs := []float64{10, 20, 30}
	lows := []float64{10, 20, 30}
	closes := []float64{10, 20, 30}
	volumes := []float64{1, 1, 8}

	got, ok := rollingVWAP(highs, lows, closes, volumes, 3)
	if !ok {
		t.Fatal("rollingVWAP returned false")
	}
	if got != 27 {
		t.Fatalf("rollingVWAP = %v, want 27", got)
	}
}

func TestVolumeStateDetectsSpikeAndDry(t *testing.T) {
	if got := volumeState(2.1, true); got != "spike" {
		t.Fatalf("volumeState spike = %q", got)
	}
	if got := volumeState(-1.1, true); got != "dry" {
		t.Fatalf("volumeState dry = %q", got)
	}
	if got := volumeState(0, false); got != "normal" {
		t.Fatalf("volumeState unavailable = %q", got)
	}
}

func TestVolumeActionHelpers(t *testing.T) {
	closes := []float64{100, 101, 102, 103, 104, 108}
	volumes := []float64{100, 100, 100, 100, 100, 300}
	ratio, ok := volumeRatio(volumes, 5)
	if !ok {
		t.Fatal("volumeRatio returned false")
	}
	if ratio != 3 {
		t.Fatalf("volumeRatio = %v, want 3", ratio)
	}
	if got := priceVolumeAction(closes, volumes, ratio, ok); got != "volume_expansion_up" {
		t.Fatalf("priceVolumeAction = %q, want volume_expansion_up", got)
	}

	highs := []float64{100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 119, 120}
	breakoutCloses := []float64{99, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 121}
	breakoutVolumes := []float64{100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 120}
	breakoutRatio, ok := volumeBreakoutRatio(breakoutVolumes, 20)
	if !ok {
		t.Fatal("volumeBreakoutRatio returned false")
	}
	if got := breakoutVolumeConfirm(highs, breakoutCloses, breakoutRatio, ok); got != "confirm" {
		t.Fatalf("breakoutVolumeConfirm = %q, want confirm", got)
	}
	if got := breakoutVolumeStrength(1.6, true); got != "strong" {
		t.Fatalf("breakoutVolumeStrength = %q", got)
	}
}

func TestVolumeDivergenceAndPhase(t *testing.T) {
	closes := []float64{100, 101, 102, 103, 104, 110}
	volumes := []float64{100, 120, 140, 160, 200, 100}
	if got := volumeDivergence(closes, volumes, 6); got != "bearish" {
		t.Fatalf("volumeDivergence = %q, want bearish", got)
	}
	if got := volumePhase(0.2, 0.1, true); got != "accumulation" {
		t.Fatalf("volumePhase = %q, want accumulation", got)
	}
	if got := volumeState(3.1, true); got != "climax" {
		t.Fatalf("volumeState climax = %q", got)
	}
}

func TestPriceVolumeConfirmationDetectsBearishDivergence(t *testing.T) {
	closes := []float64{100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 121}
	obvValues := []float64{100, 120, 140, 160, 180, 200, 220, 240, 260, 280, 300, 320, 340, 360, 380, 400, 390, 380, 370, 360}
	pvtValues := []float64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 24, 23, 22, 21}

	got := priceVolumeConfirmation(closes, obvValues, pvtValues)
	if got != "divergence_bear" {
		t.Fatalf("priceVolumeConfirmation = %q, want divergence_bear", got)
	}
}

func TestChaikinMoneyFlowAndADLine(t *testing.T) {
	highs := []float64{10, 11, 12, 13, 14}
	lows := []float64{8, 9, 10, 11, 12}
	closes := []float64{9.8, 10.8, 11.8, 12.8, 13.8}
	volumes := []float64{100, 100, 100, 100, 100}

	cmf, ok := chaikinMoneyFlow(highs, lows, closes, volumes, 5)
	if !ok {
		t.Fatal("chaikinMoneyFlow returned false")
	}
	if cmf <= 0 {
		t.Fatalf("cmf = %v, want positive", cmf)
	}
	adValues := accumulationDistributionSeries(highs, lows, closes, volumes)
	if len(adValues) != len(closes) || adValues[len(adValues)-1] <= 0 {
		t.Fatalf("unexpected ad values: %#v", adValues)
	}
}

func moneyFlowSeries(length int, start float64, step float64, volume float64) ([]float64, []float64, []float64, []float64) {
	highs := make([]float64, 0, length)
	lows := make([]float64, 0, length)
	closes := make([]float64, 0, length)
	volumes := make([]float64, 0, length)
	for index := 0; index < length; index++ {
		closeValue := start + float64(index)*step
		highs = append(highs, closeValue+1)
		lows = append(lows, closeValue-1)
		closes = append(closes, closeValue)
		volumes = append(volumes, volume+float64(index))
	}
	return highs, lows, closes, volumes
}

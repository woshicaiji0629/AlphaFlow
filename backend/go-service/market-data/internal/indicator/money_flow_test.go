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
		"obv_slope5",
		"volume_zscore20",
		"volume_pressure20",
		"price_volume_trend",
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

func TestPriceVolumeConfirmationDetectsBearishDivergence(t *testing.T) {
	closes := []float64{100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 121}
	obvValues := []float64{100, 120, 140, 160, 180, 200, 220, 240, 260, 280, 300, 320, 340, 360, 380, 400, 390, 380, 370, 360}
	pvtValues := []float64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 24, 23, 22, 21}

	got := priceVolumeConfirmation(closes, obvValues, pvtValues)
	if got != "divergence_bear" {
		t.Fatalf("priceVolumeConfirmation = %q, want divergence_bear", got)
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

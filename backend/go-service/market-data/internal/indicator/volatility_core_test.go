package indicator

import "testing"

func TestATRAndADXUseWilderSmoothing(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 1)

	atrValue, ok := atr(highs, lows, closes, 14)
	if !ok {
		t.Fatal("atr returned false")
	}
	if atrValue <= 0 {
		t.Fatalf("atr = %v, want positive", atrValue)
	}
	adxValue, plusDI, minusDI, ok := adx(highs, lows, closes, 14)
	if !ok {
		t.Fatal("adx returned false")
	}
	if adxValue <= 0 || plusDI <= minusDI {
		t.Fatalf("unexpected adx values: adx=%v plus=%v minus=%v", adxValue, plusDI, minusDI)
	}
}

func TestVolatilityCoreFeatures(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 1)
	values := map[string]string{}
	signals := map[string]string{}

	addVolatilityCoreFeatures(values, signals, highs, lows, closes, 14)

	for _, key := range []string{"atr14", "atr_pct14", "natr14", "adx14", "di_plus14", "di_minus14"} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["adx_trend_strength"] == "" || signals["di_direction"] != "bull" || signals["volatility_state"] == "" {
		t.Fatalf("unexpected volatility signals: %#v", signals)
	}
}

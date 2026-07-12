package indicatorcalc

import "testing"

func TestATRAndADXUseTradingViewScriptSmoothing(t *testing.T) {
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

func TestATRSeriesFromTrueRangesMatchesDirectCalculation(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 1)
	direct, directOK := atrSeries(highs, lows, closes, 14)
	shared, sharedOK := atrSeriesFromTrueRanges(trueRanges(highs, lows, closes), 14)
	if directOK != sharedOK || len(direct) != len(shared) {
		t.Fatalf("result shape differs: direct=%v/%d shared=%v/%d", directOK, len(direct), sharedOK, len(shared))
	}
	for index := range direct {
		if direct[index] != shared[index] {
			t.Fatalf("value[%d] = %v, want %v", index, shared[index], direct[index])
		}
	}
}

func TestDirectionalMovementMatchesTradingViewFormula(t *testing.T) {
	plus := directionalMovementPlus(12, 10, 8, 9)
	minus := directionalMovementMinus(12, 10, 8, 9)

	if plus != 2 {
		t.Fatalf("plus dm = %v, want 2", plus)
	}
	if minus != 0 {
		t.Fatalf("minus dm = %v, want 0", minus)
	}

	plus = directionalMovementPlus(10, 12, 7, 9)
	minus = directionalMovementMinus(10, 12, 7, 9)

	if plus != 0 {
		t.Fatalf("plus dm = %v, want 0", plus)
	}
	if minus != 2 {
		t.Fatalf("minus dm = %v, want 2", minus)
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

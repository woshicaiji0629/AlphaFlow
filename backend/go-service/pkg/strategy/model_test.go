package strategy

import "testing"

func TestIndicatorViewFloatPrefersNumericValue(t *testing.T) {
	view := IndicatorView{
		Values:        map[string]string{"rsi14": "40"},
		NumericValues: map[string]float64{"rsi14": 55.5},
	}
	value, ok := view.Float("rsi14")
	if !ok || value != 55.5 {
		t.Fatalf("Float = %v/%v, want 55.5/true", value, ok)
	}
}

func TestIndicatorViewFloatFallsBackToLegacyValue(t *testing.T) {
	view := IndicatorView{Values: map[string]string{"rsi14": "42.25"}}
	value, ok := view.Float("rsi14")
	if !ok || value != 42.25 {
		t.Fatalf("Float = %v/%v, want 42.25/true", value, ok)
	}
	if _, ok := view.Float("missing"); ok {
		t.Fatal("missing value reported as available")
	}
}

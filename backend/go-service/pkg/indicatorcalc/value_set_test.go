package indicatorcalc

import (
	"math"
	"testing"
)

func TestValueSetFiltersInvalidValuesAndEncodesAtBoundary(t *testing.T) {
	set := NewValueSet(3)
	set.Set("rsi14", 55.25)
	set.Set("nan", math.NaN())
	set.Set("inf", math.Inf(1))

	if got := set.Numeric()["rsi14"]; got != 55.25 {
		t.Fatalf("rsi14 = %v, want 55.25", got)
	}
	if _, ok := set.Numeric()["nan"]; ok {
		t.Fatal("NaN should be omitted")
	}
	if got := set.EncodeStrings()["rsi14"]; got != "55.25" {
		t.Fatalf("encoded rsi14 = %q, want 55.25", got)
	}
}

func TestValueSetFromStringsKeepsOnlyNumericFields(t *testing.T) {
	set := ValueSetFromStrings(map[string]string{"rsi14": "42.5", "state": "bullish"})
	if got := set.Numeric()["rsi14"]; got != 42.5 {
		t.Fatalf("rsi14 = %v, want 42.5", got)
	}
	if _, ok := set.Numeric()["state"]; ok {
		t.Fatal("non-numeric field should be omitted")
	}
}

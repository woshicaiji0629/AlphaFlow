package indicatorcalc

import "testing"

func TestPivotPointFeatures(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(40, 100, 0.8)
	values := map[string]string{}
	signals := map[string]string{}

	addPivotPointFeatures(values, signals, highs, lows, closes)

	for _, key := range []string{"pivot_point", "pivot_r1", "pivot_r2", "pivot_s1", "pivot_s2"} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["pivot_zone"] == "" {
		t.Fatalf("missing pivot_zone: %#v", signals)
	}
}

func TestPivotZone(t *testing.T) {
	if got := pivotZone(100.1, 100); got != "near_pivot" {
		t.Fatalf("pivotZone near = %q", got)
	}
	if got := pivotZone(101, 100); got != "above_pivot" {
		t.Fatalf("pivotZone above = %q", got)
	}
	if got := pivotZone(99, 100); got != "below_pivot" {
		t.Fatalf("pivotZone below = %q", got)
	}
}

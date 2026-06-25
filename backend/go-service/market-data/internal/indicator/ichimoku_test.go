package indicator

import "testing"

func TestIchimokuFeatures(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 0.8)
	values := map[string]string{}
	signals := map[string]string{}

	addIchimokuFeatures(values, signals, highs, lows, closes)

	for _, key := range []string{"ichimoku_tenkan", "ichimoku_kijun", "ichimoku_span_a", "ichimoku_span_b"} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	for _, key := range []string{"ichimoku_trend", "ichimoku_cloud", "ichimoku_cross"} {
		if signals[key] == "" {
			t.Fatalf("missing %s in %#v", key, signals)
		}
	}
}

func TestIchimokuCloud(t *testing.T) {
	point := ichimokuPoint{spanA: 100, spanB: 90}
	if got := ichimokuCloud(110, point); got != "above_cloud" {
		t.Fatalf("ichimokuCloud above = %q", got)
	}
	if got := ichimokuCloud(80, point); got != "below_cloud" {
		t.Fatalf("ichimokuCloud below = %q", got)
	}
	if got := ichimokuCloud(95, point); got != "inside_cloud" {
		t.Fatalf("ichimokuCloud inside = %q", got)
	}
}

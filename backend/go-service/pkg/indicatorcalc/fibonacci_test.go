package indicatorcalc

import "testing"

func TestFibonacciFeatures(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 0.8)
	values := map[string]string{}
	signals := map[string]string{}

	addFibonacciFeatures(values, signals, highs, lows, closes)

	for _, key := range []string{"fib_236", "fib_382", "fib_5", "fib_618", "fib_786"} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["fib_zone"] == "" {
		t.Fatalf("missing fib_zone: %#v", signals)
	}
}

func TestRetracementLevels(t *testing.T) {
	up := retracementLevels(100, 200, true)
	if up[2] != 150 {
		t.Fatalf("up fib 0.5 = %v, want 150", up[2])
	}
	down := retracementLevels(100, 200, false)
	if down[2] != 150 {
		t.Fatalf("down fib 0.5 = %v, want 150", down[2])
	}
	if got := fibonacciZone(150, up); got != "near_5" {
		t.Fatalf("fibonacciZone = %q, want near_5", got)
	}
}

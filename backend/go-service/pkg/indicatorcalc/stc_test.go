package indicatorcalc

import (
	"math"
	"testing"
)

func TestSTCStreamingMatchesBatch(t *testing.T) {
	closes := make([]float64, 160)
	for index := range closes {
		closes[index] = 100 + 0.08*float64(index) + 4*math.Sin(float64(index)/7)
	}
	state := newStreamSTCState()
	for _, closeValue := range closes {
		state.append(closeValue)
	}
	batch, batchPrevious, ok := stcValue(closes)
	if !ok {
		t.Fatal("stcValue() not ready")
	}
	if math.Abs(state.current-batch) > 1e-12 || math.Abs(state.previous-batchPrevious) > 1e-12 {
		t.Fatalf("stream=(%v,%v), batch=(%v,%v)", state.current, state.previous, batch, batchPrevious)
	}
}

func TestSTCRemainsFiniteAndBounded(t *testing.T) {
	state := newStreamSTCState()
	for index := 0; index < 200; index++ {
		state.append(100)
	}
	if state.pointCount == 0 {
		t.Fatal("STC never became ready")
	}
	if math.IsNaN(state.current) || math.IsInf(state.current, 0) || state.current < 0 || state.current > 100 {
		t.Fatalf("STC = %v, want finite value in [0,100]", state.current)
	}
}

func TestSTCSignals(t *testing.T) {
	if got := stcCross(30, 20); got != "up_25" {
		t.Fatalf("up cross = %q", got)
	}
	if got := stcCross(70, 80); got != "down_75" {
		t.Fatalf("down cross = %q", got)
	}
	if got := stcZone(80); got != "overbought" {
		t.Fatalf("zone = %q", got)
	}
	if got := stcDirection(51, 50); got != "rising" {
		t.Fatalf("direction = %q", got)
	}
}

func TestCalculatePublishesSTCFeatures(t *testing.T) {
	result, err := Calculate(benchmarkKlines(120), DefaultOptions())
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	value, ok := result.NumericValues["stc"]
	if !ok || value < 0 || value > 100 {
		t.Fatalf("stc = %v, present = %v", value, ok)
	}
	if _, ok := result.NumericValues["stc_delta"]; !ok {
		t.Fatal("stc_delta missing")
	}
	for _, key := range []string{"stc_direction", "stc_zone", "stc_cross"} {
		if result.Signals[key] == "" {
			t.Fatalf("%s missing", key)
		}
	}
}

func BenchmarkStreamSTCAppend(b *testing.B) {
	state := newStreamSTCState()
	for index := 0; index < 100; index++ {
		state.append(100 + math.Sin(float64(index)/7))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		state.append(100 + math.Sin(float64(index)/7))
	}
}

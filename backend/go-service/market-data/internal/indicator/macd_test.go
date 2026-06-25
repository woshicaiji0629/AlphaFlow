package indicator

import "testing"

func TestMACDSeriesReturnsPoints(t *testing.T) {
	values := linearValues(80, 100, 0.5)

	series, ok := macdSeries(values, 12, 26, 9)
	if !ok {
		t.Fatal("macdSeries returned false")
	}
	if len(series) == 0 {
		t.Fatal("empty macd series")
	}
	last := series[len(series)-1]
	if last.value == 0 || last.signal == 0 {
		t.Fatalf("unexpected macd point: %#v", last)
	}
}

func TestMACDSignals(t *testing.T) {
	golden := []macdPoint{
		{value: -1, signal: 0, hist: -1},
		{value: 1, signal: 0, hist: 1},
	}
	if got := macdCross(golden); got != "golden" {
		t.Fatalf("macdCross golden = %q", got)
	}
	dead := []macdPoint{
		{value: 1, signal: 0, hist: 1},
		{value: -1, signal: 0, hist: -1},
	}
	if got := macdCross(dead); got != "dead" {
		t.Fatalf("macdCross dead = %q", got)
	}
	if got := macdMomentum(golden); got != "expanding_bull" {
		t.Fatalf("macdMomentum = %q, want expanding_bull", got)
	}
	if got := macdMomentum(dead); got != "expanding_bear" {
		t.Fatalf("macdMomentum = %q, want expanding_bear", got)
	}
}

func TestAddMACDFeatures(t *testing.T) {
	values := map[string]string{}
	signals := map[string]string{}

	addMACDFeatures(values, signals, linearValues(80, 100, 0.5), 12, 26, 9)

	if values["macd_hist_delta"] == "" {
		t.Fatalf("missing macd_hist_delta: %#v", values)
	}
	if values["macd_zero_distance"] == "" {
		t.Fatalf("missing macd_zero_distance: %#v", values)
	}
	if signals["macd_cross"] == "" || signals["macd_zone"] == "" || signals["macd_momentum"] == "" {
		t.Fatalf("missing macd signals: %#v", signals)
	}
}

func linearValues(length int, start float64, step float64) []float64 {
	values := make([]float64, 0, length)
	for index := 0; index < length; index++ {
		values = append(values, start+float64(index)*step)
	}
	return values
}

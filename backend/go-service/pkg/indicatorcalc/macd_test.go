package indicatorcalc

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
	if got := macdHistPhase([]macdPoint{{hist: 1}, {hist: 2}}); got != "above_rising" {
		t.Fatalf("macdHistPhase = %q, want above_rising", got)
	}
	if got := macdHistPhase([]macdPoint{{hist: 2}, {hist: 1}}); got != "above_falling" {
		t.Fatalf("macdHistPhase = %q, want above_falling", got)
	}
	if got := macdHistPhase([]macdPoint{{hist: -1}, {hist: -2}}); got != "below_falling" {
		t.Fatalf("macdHistPhase = %q, want below_falling", got)
	}
	if got := macdHistPhase([]macdPoint{{hist: -2}, {hist: -1}}); got != "below_rising" {
		t.Fatalf("macdHistPhase = %q, want below_rising", got)
	}
	if got := macdSignalSide(macdPoint{value: 2, signal: 1}); got != "above_signal" {
		t.Fatalf("macdSignalSide = %q, want above_signal", got)
	}
	if got := macdSignalSide(macdPoint{value: 1, signal: 2}); got != "below_signal" {
		t.Fatalf("macdSignalSide = %q, want below_signal", got)
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
	if signals["macd_cross"] == "" || signals["macd_zone"] == "" || signals["macd_momentum"] == "" ||
		signals["macd_hist_phase"] == "" || signals["macd_signal_side"] == "" || signals["macd_divergence"] == "" {
		t.Fatalf("missing macd signals: %#v", signals)
	}
}

func TestAddMACDFeaturesWithPrefix(t *testing.T) {
	values := map[string]string{}
	signals := map[string]string{}

	addMACDFeaturesWithPrefix(values, signals, linearValues(80, 100, 0.5), 7, 19, 9, "macd_fast")

	if values["macd_fast_hist_delta"] == "" {
		t.Fatalf("missing macd_fast_hist_delta: %#v", values)
	}
	if values["macd_fast_zero_distance"] == "" {
		t.Fatalf("missing macd_fast_zero_distance: %#v", values)
	}
	if signals["macd_fast_cross"] == "" || signals["macd_fast_zone"] == "" || signals["macd_fast_momentum"] == "" ||
		signals["macd_fast_hist_phase"] == "" || signals["macd_fast_signal_side"] == "" || signals["macd_fast_divergence"] == "" {
		t.Fatalf("missing fast macd signals: %#v", signals)
	}
}

func TestMACDDivergence(t *testing.T) {
	closes := []float64{100, 105, 102, 108, 104, 112, 106, 116, 110, 120}
	series := []macdPoint{
		{hist: 1},
		{hist: 4},
		{hist: 2},
		{hist: 3},
		{hist: 1},
		{hist: 2},
		{hist: 0.5},
		{hist: 1},
		{hist: 0.4},
		{hist: 0.6},
	}
	if got := macdDivergence(closes, series); got != "none" {
		t.Fatalf("short macdDivergence = %q, want none", got)
	}
	closes = []float64{100, 101, 102, 103, 110, 104, 103, 102, 101, 102, 103, 104, 120, 105, 104, 103, 102, 103, 104, 105, 130, 106, 105, 104, 103, 104, 105, 106, 140, 107, 106, 105, 104, 105}
	series = make([]macdPoint, len(closes))
	histValues := []float64{0, 1, 2, 3, 8, 3, 2, 1, 0, 1, 2, 3, 6, 3, 2, 1, 0, 1, 2, 3, 4, 3, 2, 1, 0, 1, 2, 3, 2, 1, 0, -1, 0, 1}
	for index := range series {
		series[index] = macdPoint{hist: histValues[index]}
	}
	if got := macdDivergence(closes, series); got != "bearish" {
		t.Fatalf("macdDivergence = %q, want bearish", got)
	}
}

func TestMACDHistPivotsMatchValuePivots(t *testing.T) {
	hist := []float64{1, 3, 2, 4, 1, 2, 0, 3, 1}
	points := make([]macdPoint, len(hist))
	for index := range hist {
		points[index].hist = hist[index]
	}
	wantHighs, wantLows := valuePivots(hist, 2)
	gotHighs, gotLows := macdHistPivots(points, 2)
	if len(gotHighs) != len(wantHighs) || len(gotLows) != len(wantLows) {
		t.Fatalf("pivot lengths differ: got %d/%d want %d/%d", len(gotHighs), len(gotLows), len(wantHighs), len(wantLows))
	}
	for index := range wantHighs {
		if gotHighs[index] != wantHighs[index] {
			t.Fatalf("high[%d] = %#v, want %#v", index, gotHighs[index], wantHighs[index])
		}
	}
	for index := range wantLows {
		if gotLows[index] != wantLows[index] {
			t.Fatalf("low[%d] = %#v, want %#v", index, gotLows[index], wantLows[index])
		}
	}
}

func linearValues(length int, start float64, step float64) []float64 {
	values := make([]float64, 0, length)
	for index := 0; index < length; index++ {
		values = append(values, start+float64(index)*step)
	}
	return values
}

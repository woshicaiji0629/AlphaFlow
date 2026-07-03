package indicatorcalc

import "testing"

func TestSupertrendUsesSeriesDirection(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(80, 100, 0.8)
	values := map[string]string{}
	signals := map[string]string{}

	addSupertrend(values, signals, highs, lows, closes, 10, 3)
	addAlphaTrend(values, signals, highs, lows, closes, volumes, 14, 1)
	addPSARFeatures(values, signals, highs, lows, closes)
	addChandelierExit(values, signals, highs, lows, closes, 22, 3)

	if values["supertrend"] == "" {
		t.Fatalf("missing supertrend: %#v", values)
	}
	for _, key := range []string{
		"supertrend_distance_pct",
		"supertrend_stop_distance_pct",
		"supertrend_7_2",
		"supertrend_10_3",
		"supertrend_10_3_3",
		"supertrend_14_4",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s: %#v", key, values)
		}
	}
	if signals["supertrend_direction"] != "up" {
		t.Fatalf("supertrend_direction = %q, want up", signals["supertrend_direction"])
	}
	if signals["supertrend_flip"] == "" {
		t.Fatalf("missing supertrend flip: %#v", signals)
	}
	if signals["supertrend_7_2_direction"] == "" || signals["supertrend_10_3_direction"] == "" || signals["supertrend_10_3_3_direction"] == "" || signals["supertrend_14_4_direction"] == "" {
		t.Fatalf("missing supertrend preset directions: %#v", signals)
	}
	if values["alphatrend"] == "" || values["mfi14"] == "" {
		t.Fatalf("missing alphatrend values: %#v", values)
	}
	if values["alphatrend_distance_pct"] == "" || values["alphatrend_slope_pct"] == "" {
		t.Fatalf("missing alphatrend distance/slope: %#v", values)
	}
	if signals["alphatrend_direction"] == "" {
		t.Fatalf("missing alphatrend direction: %#v", signals)
	}
	if signals["alphatrend_flip"] == "" {
		t.Fatalf("missing alphatrend flip: %#v", signals)
	}
	if signals["alphatrend_cross"] == "" || signals["alphatrend_signal"] == "" {
		t.Fatalf("missing alphatrend crossover signals: %#v", signals)
	}
	if values["psar"] == "" || values["psar_distance_pct"] == "" {
		t.Fatalf("missing psar values: %#v", values)
	}
	if signals["psar_direction"] != "up" {
		t.Fatalf("psar_direction = %q, want up", signals["psar_direction"])
	}
	if values["chandelier_long"] == "" || values["chandelier_short"] == "" || values["chandelier_stop_distance_pct"] == "" {
		t.Fatalf("missing chandelier values: %#v", values)
	}
	if signals["chandelier_direction"] == "" {
		t.Fatalf("missing chandelier direction: %#v", signals)
	}
}

func TestAlphaTrendSignalsRequirePreviousSameAndOppositeCross(t *testing.T) {
	points := trendPointsFromValues(10, 9, 11, 8, 12)

	cross, signal := alphaTrendSignals(points)

	if cross != "buy" {
		t.Fatalf("alphatrend cross = %q, want buy", cross)
	}
	if signal != "none" {
		t.Fatalf("alphatrend signal = %q, want none", signal)
	}
}

func TestAlphaTrendSignalsAllowAlternatingBuy(t *testing.T) {
	points := trendPointsFromValues(10, 9, 11, 8, 12, 9, 10, 8, 11)

	cross, signal := alphaTrendSignals(points)

	if cross != "buy" {
		t.Fatalf("alphatrend cross = %q, want buy", cross)
	}
	if signal != "buy" {
		t.Fatalf("alphatrend signal = %q, want buy", signal)
	}
}

func TestLivermoreFeaturesOutputForLongSeries(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(430, 100, 0.3)
	opens := make([]float64, 0, len(closes))
	for _, closeValue := range closes {
		opens = append(opens, closeValue-0.1)
	}
	values := map[string]string{}
	signals := map[string]string{}

	addLivermoreFeatures(values, signals, highs, lows, closes, opens)

	if signals["livermore_trend"] == "" || signals["livermore_signal"] == "" {
		t.Fatalf("missing livermore signals: %#v", signals)
	}
	if values["livermore_active_point"] == "" {
		t.Fatalf("missing livermore active point: %#v", values)
	}
}

func TestSqueezeMomentumOutputsDelta(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 0.35)
	values := map[string]string{}
	signals := map[string]string{}

	addSqueezeMomentum(values, signals, highs, lows, closes)

	if values["squeeze_momentum"] == "" {
		t.Fatalf("missing squeeze_momentum: %#v", values)
	}
	if values["squeeze_momentum_delta"] == "" {
		t.Fatalf("missing squeeze_momentum_delta: %#v", values)
	}
	if signals["squeeze"] == "" || signals["momentum_state"] == "" || signals["squeeze_state"] == "" {
		t.Fatalf("missing squeeze signals: %#v", signals)
	}
}

func TestDynamicSwingAnchoredVWAPOutputsState(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(90, 100, 0.4)
	values := map[string]string{}
	signals := map[string]string{}

	addDynamicSwingAnchoredVWAP(values, signals, highs, lows, closes, volumes)

	for _, key := range []string{
		"dynamic_swing_vwap",
		"dynamic_swing_vwap_distance_pct",
		"dynamic_swing_vwap_anchor_price",
		"dynamic_swing_vwap_anchor_age",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	for _, key := range []string{
		"dynamic_swing_vwap_direction",
		"dynamic_swing_vwap_position",
		"dynamic_swing_vwap_anchor_type",
		"dynamic_swing_vwap_swing_label",
	} {
		if signals[key] == "" {
			t.Fatalf("missing %s in %#v", key, signals)
		}
	}
}

func trendPointsFromValues(values ...float64) []trendPoint {
	points := make([]trendPoint, 0, len(values))
	for _, value := range values {
		points = append(points, trendPoint{value: value})
	}
	return points
}

func TestSqueezeState(t *testing.T) {
	if got := squeezeState("on", 1, 2); got != "squeeze_on" {
		t.Fatalf("squeezeState on = %q, want squeeze_on", got)
	}
	if got := squeezeState("released", 2, 1); got != "release_up" {
		t.Fatalf("squeezeState release up = %q, want release_up", got)
	}
	if got := squeezeState("released", -2, -1); got != "release_down" {
		t.Fatalf("squeezeState release down = %q, want release_down", got)
	}
	if got := squeezeState("off", 0, 0); got != "off_flat" {
		t.Fatalf("squeezeState off flat = %q, want off_flat", got)
	}
}

func TestBollingerShapeFeatures(t *testing.T) {
	_, _, closes, _ := trendingSeries(80, 100, 0.35)
	values := map[string]string{}
	signals := map[string]string{}

	addBollingerFeatures(values, signals, closes)

	for _, key := range []string{
		"bb_width_pct",
		"bb_percent_b",
		"bb_width_delta",
		"bb_middle_slope_pct",
		"bb_upper_slope_pct",
		"bb_lower_slope_pct",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["bb_width_state"] == "" || signals["bb_trend"] == "" {
		t.Fatalf("missing bollinger signals: %#v", signals)
	}
}

func TestBollingerStateHelpers(t *testing.T) {
	if got := bollingerWidthState(2, 10); got != "expanding" {
		t.Fatalf("bollingerWidthState expanding = %q", got)
	}
	if got := bollingerWidthState(-2, 10); got != "contracting" {
		t.Fatalf("bollingerWidthState contracting = %q", got)
	}
	if got := bollingerTrend(0.01); got != "flat" {
		t.Fatalf("bollingerTrend flat = %q", got)
	}
}

func TestChannelFeatures(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 0.35)
	values := map[string]string{}
	signals := map[string]string{}

	addChannelFeatures(values, signals, highs, lows, closes)

	for _, key := range []string{
		"donchian_high20",
		"donchian_low20",
		"donchian_mid20",
		"donchian_width_pct20",
		"donchian_position20",
		"keltner_upper20",
		"keltner_middle20",
		"keltner_lower20",
		"keltner_width_pct20",
		"keltner_position20",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["donchian_breakout"] == "" || signals["keltner_breakout"] == "" {
		t.Fatalf("missing channel signals: %#v", signals)
	}
}

func TestChannelBreakout(t *testing.T) {
	if got := channelBreakout(11, 10, 5); got != "breakout_up" {
		t.Fatalf("channelBreakout up = %q", got)
	}
	if got := channelBreakout(4, 10, 5); got != "breakout_down" {
		t.Fatalf("channelBreakout down = %q", got)
	}
	if got := channelBreakout(7, 10, 5); got != "inside" {
		t.Fatalf("channelBreakout inside = %q", got)
	}
}

func TestDonchianBreakoutUsesPreviousChannel(t *testing.T) {
	highs := linearValues(21, 10, 1)
	lows := linearValues(21, 5, 1)
	closes := linearValues(21, 7, 1)
	closes[len(closes)-1] = highs[len(highs)-2] + 0.5
	values := map[string]string{}
	signals := map[string]string{}

	addDonchianChannelFeatures(values, signals, highs, lows, closes, 20)

	if signals["donchian_breakout"] != "breakout_up" {
		t.Fatalf("donchian_breakout = %q, want breakout_up", signals["donchian_breakout"])
	}
}

func TestSqueezeMomentumAtUsesRangeBaseline(t *testing.T) {
	highs := []float64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	lows := []float64{9, 10, 11, 12, 13, 14, 15, 16, 17, 18}
	closes := []float64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19}

	value, ok := squeezeMomentumAt(highs, lows, closes, 5, 10)
	if !ok {
		t.Fatal("squeezeMomentumAt returned false")
	}
	if value != 2 {
		t.Fatalf("momentum = %v, want 2", value)
	}
}

func trendingSeries(length int, start float64, step float64) ([]float64, []float64, []float64, []float64) {
	highs := make([]float64, 0, length)
	lows := make([]float64, 0, length)
	closes := make([]float64, 0, length)
	volumes := make([]float64, 0, length)
	for index := 0; index < length; index++ {
		closeValue := start + float64(index)*step
		highs = append(highs, closeValue+1.2)
		lows = append(lows, closeValue-1)
		closes = append(closes, closeValue)
		volumes = append(volumes, 100+float64(index%7))
	}
	return highs, lows, closes, volumes
}

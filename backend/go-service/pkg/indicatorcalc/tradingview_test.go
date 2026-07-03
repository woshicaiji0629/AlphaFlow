package indicatorcalc

import "testing"

func TestTradingViewFeaturesOutput(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(160, 100, 0.35)
	values := map[string]string{}
	signals := map[string]string{}

	addTradingViewFeatures(values, signals, highs, lows, closes)

	for _, key := range []string{
		"qqe_line",
		"qqe_signal",
		"qqe_hist",
		"ut_stop",
		"ssl_upper",
		"ssl_lower",
		"range_filter",
		"range_filter_upper",
		"range_filter_lower",
		"wvf",
		"wvf_upper_band",
		"wvf_range_high",
		"td_sell_setup_count",
		"nw_middle",
		"nw_upper",
		"nw_lower",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	for _, key := range []string{
		"qqe_trend",
		"qqe_cross",
		"ut_direction",
		"ut_signal",
		"ssl_direction",
		"ssl_cross",
		"range_filter_direction",
		"wvf_state",
		"td_exhaustion",
		"nw_trend",
		"nw_position_state",
	} {
		if signals[key] == "" {
			t.Fatalf("missing %s in %#v", key, signals)
		}
	}
}

func TestTDSequentialSetupCounts(t *testing.T) {
	_, sellCount, exhaustion := tdSequential(linearValues(13, 100, 1))
	if sellCount != 9 || exhaustion != "sell" {
		t.Fatalf("td sell setup = %d/%q, want 9/sell", sellCount, exhaustion)
	}

	down := linearValues(13, 100, -1)
	buyCount, _, exhaustion := tdSequential(down)
	if buyCount != 9 || exhaustion != "buy" {
		t.Fatalf("td buy setup = %d/%q, want 9/buy", buyCount, exhaustion)
	}
}

func TestTradingViewSignalHelpers(t *testing.T) {
	if got := directionFlipSignal("down", "up"); got != "buy" {
		t.Fatalf("directionFlipSignal buy = %q", got)
	}
	if got := directionFlipSignal("up", "down"); got != "sell" {
		t.Fatalf("directionFlipSignal sell = %q", got)
	}
	if got := directionFlipCross("bear", "bull"); got != "golden" {
		t.Fatalf("directionFlipCross golden = %q", got)
	}
	if got := directionFlipCross("bull", "bear"); got != "dead" {
		t.Fatalf("directionFlipCross dead = %q", got)
	}
	if got := williamsVixFixState(10, 8, 9); got != "panic" {
		t.Fatalf("williamsVixFixState panic = %q", got)
	}
	if got := thresholdTrend(55, 50, 50); got != "bull" {
		t.Fatalf("thresholdTrend bull = %q", got)
	}
}

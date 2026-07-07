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
		"qqe_primary_line",
		"qqe_primary_trend",
		"qqe_secondary_line",
		"qqe_secondary_trend",
		"qqe_bb_upper",
		"qqe_bb_lower",
		"qqe_primary_hist",
		"qqe_secondary_hist",
		"ut_stop",
		"ssl_upper",
		"ssl_lower",
		"range_filter",
		"range_filter_upper",
		"range_filter_lower",
		"wvf",
		"wvf_mid_line",
		"wvf_upper_band",
		"wvf_lower_band",
		"wvf_range_high",
		"wvf_range_low",
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
		"qqe_mod_signal",
		"qqe_primary_zero_cross",
		"ut_direction",
		"ut_signal",
		"ssl_direction",
		"ssl_cross",
		"range_filter_direction",
		"wvf_state",
		"wvf_zone",
		"td_exhaustion",
		"nw_trend",
		"nw_position_state",
	} {
		if signals[key] == "" {
			t.Fatalf("missing %s in %#v", key, signals)
		}
	}
}

func TestQQEModEnhancedOutputsSignalFields(t *testing.T) {
	closes := oscillatingCloses(180)

	result, ok := qqeModEnhanced(closes, 6, 5, 3, 1.61, 50, 0.35, 3)

	if !ok {
		t.Fatal("qqeModEnhanced returned false")
	}
	if result.primaryLine == 0 || result.secondaryLine == 0 {
		t.Fatalf("missing qqe lines: %#v", result)
	}
	if result.bbUpper == result.bbLower {
		t.Fatalf("bb upper/lower should differ: %#v", result)
	}
	if result.signal == "" || result.zeroCross == "" {
		t.Fatalf("missing qqe signals: %#v", result)
	}
}

func TestQQEModSignalHelpers(t *testing.T) {
	if got := qqeModSignal(5, 4, 3, -3, 3); got != "up" {
		t.Fatalf("qqeModSignal up = %q", got)
	}
	if got := qqeModSignal(-5, -4, 3, -3, 3); got != "down" {
		t.Fatalf("qqeModSignal down = %q", got)
	}
	if got := qqeZeroCross(49, 51); got != "up" {
		t.Fatalf("qqeZeroCross up = %q", got)
	}
	if got := qqeZeroCross(51, 49); got != "down" {
		t.Fatalf("qqeZeroCross down = %q", got)
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

func TestRangeFilterCompactMatchesBatch(t *testing.T) {
	closes := oscillatingCloses(180)

	gotFilter, gotUpper, gotLower, gotDirection, ok := rangeFilterCompact(closes, 100, 3)
	if !ok {
		t.Fatal("rangeFilterCompact returned false")
	}
	wantFilter, wantUpper, wantLower, wantDirection, ok := rangeFilter(closes, 100, 3)
	if !ok {
		t.Fatal("rangeFilter returned false")
	}
	assertFloatClose(t, "range filter", gotFilter, wantFilter)
	assertFloatClose(t, "range filter upper", gotUpper, wantUpper)
	assertFloatClose(t, "range filter lower", gotLower, wantLower)
	if gotDirection != wantDirection {
		t.Fatalf("range filter direction = %q, want %q", gotDirection, wantDirection)
	}
}

func oscillatingCloses(length int) []float64 {
	values := make([]float64, 0, length)
	price := 100.0
	for index := 0; index < length; index++ {
		if index%12 < 6 {
			price += 0.8
		} else {
			price -= 0.5
		}
		values = append(values, price)
	}
	return values
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
	if got := williamsVixFixZone(10, 8, 1, 9, 2); got != "panic" {
		t.Fatalf("williamsVixFixZone panic = %q", got)
	}
	if got := williamsVixFixZone(1, 8, 2, 9, 2); got != "low_volatility" {
		t.Fatalf("williamsVixFixZone low volatility = %q", got)
	}
	if got := williamsVixFixZone(4, 8, 2, 9, 1); got != "normal" {
		t.Fatalf("williamsVixFixZone normal = %q", got)
	}
	if got := thresholdTrend(55, 50, 50); got != "bull" {
		t.Fatalf("thresholdTrend bull = %q", got)
	}
}

func TestWilliamsVixFixCompactMatchesBatch(t *testing.T) {
	_, lows, closes, _ := trendingSeries(160, 100, 0.35)

	got, ok := williamsVixFixCompact(lows, closes, 22, 20, 2, 50, 0.85)
	if !ok {
		t.Fatal("williamsVixFixCompact returned false")
	}
	want, ok := williamsVixFix(lows, closes, 22, 20, 2, 50, 0.85)
	if !ok {
		t.Fatal("williamsVixFix returned false")
	}
	assertFloatClose(t, "wvf", got.value, want.value)
	assertFloatClose(t, "wvf mid", got.mid, want.mid)
	assertFloatClose(t, "wvf upper", got.upperBand, want.upperBand)
	assertFloatClose(t, "wvf lower", got.lowerBand, want.lowerBand)
	assertFloatClose(t, "wvf range high", got.rangeHigh, want.rangeHigh)
	assertFloatClose(t, "wvf range low", got.rangeLow, want.rangeLow)
}

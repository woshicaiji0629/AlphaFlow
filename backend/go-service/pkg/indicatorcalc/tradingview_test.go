package indicatorcalc

import (
	"math"
	"reflect"
	"testing"
)

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

func TestQQESharedFoundationMatchesStandalonePaths(t *testing.T) {
	closes := oscillatingCloses(180)
	rsiValues, rsiOK := rsiSeries(closes, 6)
	smoothed, smoothedDeltas, offset, foundationOK := qqeModTrendFoundation(rsiValues, rsiOK, 6, 5)

	wantValues := map[string]string{}
	wantSignals := map[string]string{}
	addQQEModFeaturesWithRSI(nil, wantValues, wantSignals, rsiValues, rsiOK, 6, 5, 3)
	addQQEModEnhancedFeaturesWithRSI(nil, wantValues, wantSignals, rsiValues, rsiOK)

	gotValues := map[string]string{}
	gotSignals := map[string]string{}
	addQQEModFeaturesWithFoundation(nil, gotValues, gotSignals, smoothed, smoothedDeltas, 6, 5, 3, foundationOK)
	addQQEModEnhancedFeaturesWithFoundation(nil, gotValues, gotSignals, smoothed, smoothedDeltas, offset, foundationOK)

	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("shared QQE values differ: got=%#v want=%#v", gotValues, wantValues)
	}
	if !reflect.DeepEqual(gotSignals, wantSignals) {
		t.Fatalf("shared QQE signals differ: got=%#v want=%#v", gotSignals, wantSignals)
	}
}

var benchmarkQQELine float64
var benchmarkQQEEnhanced qqeModEnhancedResult

func BenchmarkQQEIndependentFoundations(b *testing.B) {
	closes := oscillatingCloses(268)
	rsiValues, rsiOK := rsiSeries(closes, 6)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		line, _, _, _, _ := qqeModWithRSI(rsiValues, rsiOK, 6, 5, 3)
		enhanced, _ := qqeModEnhancedWithRSI(rsiValues, rsiOK, 6, 5, 3, 1.61, 50, 0.35, 3)
		benchmarkQQELine = line
		benchmarkQQEEnhanced = enhanced
	}
}

func BenchmarkQQESharedFoundation(b *testing.B) {
	closes := oscillatingCloses(268)
	rsiValues, rsiOK := rsiSeries(closes, 6)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		smoothed, smoothedDeltas, offset, foundationOK := qqeModTrendFoundation(rsiValues, rsiOK, 6, 5)
		line, _, _, _, _ := qqeModFromFoundation(smoothed, smoothedDeltas, 6, 5, 3, foundationOK)
		enhanced, _ := qqeModEnhancedFromFoundation(smoothed, smoothedDeltas, offset, 3, 1.61, 50, 0.35, 3, foundationOK)
		benchmarkQQELine = line
		benchmarkQQEEnhanced = enhanced
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

func TestNadarayaWatsonEnvelopeMatchesBatch(t *testing.T) {
	closes := oscillatingCloses(180)

	gotMiddle, gotMAE, gotPrevious, ok := nadarayaWatsonEnvelope(closes, 50, 8)
	if !ok {
		t.Fatal("nadarayaWatsonEnvelope returned false")
	}
	wantMiddle, wantMAE, wantPrevious, ok := referenceNadarayaWatsonEnvelope(closes, 50, 8)
	if !ok {
		t.Fatal("referenceNadarayaWatsonEnvelope returned false")
	}

	assertFloatClose(t, "nw middle", gotMiddle, wantMiddle)
	assertFloatClose(t, "nw mae", gotMAE, wantMAE)
	assertFloatClose(t, "nw previous", gotPrevious, wantPrevious)
}

func referenceNadarayaWatsonEnvelope(closes []float64, length int, bandwidth float64) (float64, float64, float64, bool) {
	if length <= 1 || bandwidth <= 0 || len(closes) < length+1 {
		return 0, 0, 0, false
	}
	middle, ok := nadarayaWatsonAtBatch(closes, length, bandwidth, len(closes))
	if !ok {
		return 0, 0, 0, false
	}
	previousMiddle, ok := nadarayaWatsonAtBatch(closes, length, bandwidth, len(closes)-1)
	if !ok {
		return 0, 0, 0, false
	}
	var errorSum float64
	start := len(closes) - length
	for index := start; index < len(closes); index++ {
		fit, fitOK := nadarayaWatsonAtBatch(closes[:index+1], minInt(length, index+1), bandwidth, index+1)
		if !fitOK {
			continue
		}
		errorSum += math.Abs(closes[index] - fit)
	}
	return middle, errorSum / float64(length), previousMiddle, true
}

func nadarayaWatsonAtBatch(values []float64, length int, bandwidth float64, end int) (float64, bool) {
	if length <= 0 || end < length || end > len(values) {
		return 0, false
	}
	start := end - length
	var weighted float64
	var weightSum float64
	for index := start; index < end; index++ {
		distance := float64(end - 1 - index)
		weight := math.Exp(-(distance * distance) / (2 * bandwidth * bandwidth))
		weighted += values[index] * weight
		weightSum += weight
	}
	if weightSum == 0 {
		return 0, false
	}
	return weighted / weightSum, true
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

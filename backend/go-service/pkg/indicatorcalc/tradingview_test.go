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

func TestStreamSSL10MatchesBatchAtEveryPoint(t *testing.T) {
	closes := oscillatingCloses(300)
	highs := make([]float64, len(closes))
	lows := make([]float64, len(closes))
	for index, closeValue := range closes {
		highs[index] = closeValue + 1 + float64(index%3)*0.1
		lows[index] = closeValue - 1 - float64(index%4)*0.1
	}
	state := newStreamSSL10State()
	for index := range closes {
		state.append(highs[index], lows[index], closes[index])
		gotUpper, gotLower, gotDirection, gotPreviousDirection, gotOK := state.value()
		wantUpper, wantLower, wantDirection, wantPreviousDirection, wantOK := sslChannel(highs[:index+1], lows[:index+1], closes[:index+1], 10)
		if gotOK != wantOK {
			t.Fatalf("index=%d stream ok = %v, want %v", index, gotOK, wantOK)
		}
		if gotOK {
			assertFloatClose(t, "stream ssl upper", gotUpper, wantUpper)
			assertFloatClose(t, "stream ssl lower", gotLower, wantLower)
			if gotDirection != wantDirection || gotPreviousDirection != wantPreviousDirection {
				t.Fatalf("index=%d direction = %q/%q, want %q/%q", index, gotDirection, gotPreviousDirection, wantDirection, wantPreviousDirection)
			}
		}
	}
}

func TestSSL10WithStateMatchesStandaloneFeatures(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(160, 100, 0.35)
	basic := buildBasicIndicatorState(highs, lows, closes, volumes)
	wantValues, wantSignals := map[string]string{}, map[string]string{}
	addSSLChannelFeatures(nil, wantValues, wantSignals, highs, lows, closes, 10, nil)
	gotValues, gotSignals := map[string]string{}, map[string]string{}
	addSSLChannelFeatures(nil, gotValues, gotSignals, highs, lows, closes, 10, basic)
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("state SSL values differ: got=%#v want=%#v", gotValues, wantValues)
	}
	if !reflect.DeepEqual(gotSignals, wantSignals) {
		t.Fatalf("state SSL signals differ: got=%#v want=%#v", gotSignals, wantSignals)
	}
}

func TestStreamRangeFilter100MatchesBatchAtEveryPoint(t *testing.T) {
	closes := oscillatingCloses(300)
	state := newStreamRangeFilterState(100, 3)
	for index, closeValue := range closes {
		state.append(closeValue)
		gotFilter, gotUpper, gotLower, gotDirection, gotOK := state.value()
		wantFilter, wantUpper, wantLower, wantDirection, wantOK := rangeFilterCompact(closes[:index+1], 100, 3)
		if gotOK != wantOK {
			t.Fatalf("index=%d stream ok = %v, want %v", index, gotOK, wantOK)
		}
		if gotOK {
			assertFloatClose(t, "stream range filter", gotFilter, wantFilter)
			assertFloatClose(t, "stream range filter upper", gotUpper, wantUpper)
			assertFloatClose(t, "stream range filter lower", gotLower, wantLower)
			if gotDirection != wantDirection {
				t.Fatalf("index=%d direction = %q, want %q", index, gotDirection, wantDirection)
			}
		}
	}
}

func TestRangeFilter100WithStateMatchesStandaloneFeatures(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(160, 100, 0.35)
	basic := buildBasicIndicatorState(highs, lows, closes, volumes)
	wantValues, wantSignals := map[string]string{}, map[string]string{}
	addRangeFilterFeatures(nil, wantValues, wantSignals, closes, 100, 3, nil)
	gotValues, gotSignals := map[string]string{}, map[string]string{}
	addRangeFilterFeatures(nil, gotValues, gotSignals, closes, 100, 3, basic)
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("state range filter values differ: got=%#v want=%#v", gotValues, wantValues)
	}
	if !reflect.DeepEqual(gotSignals, wantSignals) {
		t.Fatalf("state range filter signals differ: got=%#v want=%#v", gotSignals, wantSignals)
	}
}

func TestStreamWilliamsVixFixMatchesBatchAtEveryPoint(t *testing.T) {
	_, lows, closes, _ := trendingSeries(180, 100, 0.35)
	var state streamWilliamsVixFixState
	for index := range closes {
		state.append(lows[index], closes[index])
		got, gotOK := state.value()
		want, wantOK := williamsVixFixCompact(lows[:index+1], closes[:index+1], 22, 20, 2, 50, 0.85)
		if gotOK != wantOK {
			t.Fatalf("index=%d stream ok = %v, want %v", index, gotOK, wantOK)
		}
		if gotOK {
			assertFloatClose(t, "stream wvf", got.value, want.value)
			assertFloatClose(t, "stream wvf mid", got.mid, want.mid)
			assertFloatClose(t, "stream wvf upper", got.upperBand, want.upperBand)
			assertFloatClose(t, "stream wvf lower", got.lowerBand, want.lowerBand)
			assertFloatClose(t, "stream wvf range high", got.rangeHigh, want.rangeHigh)
			assertFloatClose(t, "stream wvf range low", got.rangeLow, want.rangeLow)
		}
	}
}

func TestWilliamsVixFixWithStateMatchesStandaloneFeatures(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(180, 100, 0.35)
	basic := buildBasicIndicatorState(highs, lows, closes, volumes)
	wantValues, wantSignals := map[string]string{}, map[string]string{}
	addWilliamsVixFixFeatures(nil, wantValues, wantSignals, lows, closes, 22, 20, 2, 50, 0.85, nil)
	gotValues, gotSignals := map[string]string{}, map[string]string{}
	addWilliamsVixFixFeatures(nil, gotValues, gotSignals, lows, closes, 22, 20, 2, 50, 0.85, basic)
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("state WVF values differ: got=%#v want=%#v", gotValues, wantValues)
	}
	if !reflect.DeepEqual(gotSignals, wantSignals) {
		t.Fatalf("state WVF signals differ: got=%#v want=%#v", gotSignals, wantSignals)
	}
}

func TestStreamTDSequentialMatchesBatchAtEveryPoint(t *testing.T) {
	closes := oscillatingCloses(300)
	var state streamTDSequentialState
	for index, closeValue := range closes {
		state.append(closeValue)
		gotBuy, gotSell, gotExhaustion := state.value()
		wantBuy, wantSell, wantExhaustion := tdSequential(closes[:index+1])
		if gotBuy != wantBuy || gotSell != wantSell || gotExhaustion != wantExhaustion {
			t.Fatalf("index=%d stream = %d/%d/%q, want %d/%d/%q", index, gotBuy, gotSell, gotExhaustion, wantBuy, wantSell, wantExhaustion)
		}
	}
}

func TestTDSequentialWithStateMatchesStandaloneFeatures(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(160, 100, 0.35)
	basic := buildBasicIndicatorState(highs, lows, closes, volumes)
	wantValues, wantSignals := map[string]string{}, map[string]string{}
	addTDSequentialFeatures(nil, wantValues, wantSignals, closes, nil)
	gotValues, gotSignals := map[string]string{}, map[string]string{}
	addTDSequentialFeatures(nil, gotValues, gotSignals, closes, basic)
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("state TD values differ: got=%#v want=%#v", gotValues, wantValues)
	}
	if !reflect.DeepEqual(gotSignals, wantSignals) {
		t.Fatalf("state TD signals differ: got=%#v want=%#v", gotSignals, wantSignals)
	}
}

func TestStreamNadarayaWatsonMatchesBatchAtEveryPoint(t *testing.T) {
	closes := oscillatingCloses(180)
	state := newStreamNadarayaWatsonState(8)
	for index, closeValue := range closes {
		state.append(closeValue)
		gotMiddle, gotMAE, gotPrevious, gotOK := state.value()
		wantMiddle, wantMAE, wantPrevious, wantOK := nadarayaWatsonEnvelope(closes[:index+1], 50, 8)
		if gotOK != wantOK {
			t.Fatalf("index=%d stream ok = %v, want %v", index, gotOK, wantOK)
		}
		if gotOK {
			assertFloatClose(t, "stream nw middle", gotMiddle, wantMiddle)
			assertFloatClose(t, "stream nw mae", gotMAE, wantMAE)
			assertFloatClose(t, "stream nw previous", gotPrevious, wantPrevious)
		}
	}
}

func TestNadarayaWatsonWithStateMatchesStandaloneFeatures(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(180, 100, 0.35)
	basic := buildBasicIndicatorState(highs, lows, closes, volumes)
	wantValues, wantSignals := map[string]string{}, map[string]string{}
	addNadarayaWatsonEnvelopeFeatures(nil, wantValues, wantSignals, closes, 50, 8, 3, nil)
	gotValues, gotSignals := map[string]string{}, map[string]string{}
	addNadarayaWatsonEnvelopeFeatures(nil, gotValues, gotSignals, closes, 50, 8, 3, basic)
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("state NW values differ: got=%#v want=%#v", gotValues, wantValues)
	}
	if !reflect.DeepEqual(gotSignals, wantSignals) {
		t.Fatalf("state NW signals differ: got=%#v want=%#v", gotSignals, wantSignals)
	}
}

func TestStreamUTBot10MatchesBatchAtEveryPoint(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(180, 100, 0.35)
	atr := newStreamATRState(10)
	state := newStreamUTBotState(1)
	for index := range closes {
		if index > 0 {
			atr.append(highs[index], lows[index], closes[index-1])
		}
		state.append(closes[index], atr.value, atr.ready)
		gotStop, gotDirection, gotPreviousDirection, gotOK := state.value()
		wantStop, wantDirection, wantPreviousDirection, wantOK := utBot(highs[:index+1], lows[:index+1], closes[:index+1], 10, 1)
		if gotOK != wantOK {
			t.Fatalf("index=%d stream ok = %v, want %v", index, gotOK, wantOK)
		}
		if gotOK {
			assertFloatClose(t, "stream ut stop", gotStop, wantStop)
			if gotDirection != wantDirection || gotPreviousDirection != wantPreviousDirection {
				t.Fatalf("index=%d direction = %q/%q, want %q/%q", index, gotDirection, gotPreviousDirection, wantDirection, wantPreviousDirection)
			}
		}
	}
}

func TestUTBot10WithStateMatchesStandaloneFeatures(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(180, 100, 0.35)
	basic := buildBasicIndicatorState(highs, lows, closes, volumes)
	atrValues, ok := atrSeries(highs, lows, closes, 10)
	if !ok {
		t.Fatal("missing ATR10")
	}
	wantValues, wantSignals := map[string]string{}, map[string]string{}
	addUTBotFeaturesWithATR(nil, wantValues, wantSignals, closes, 10, 1, atrValues, nil)
	gotValues, gotSignals := map[string]string{}, map[string]string{}
	addUTBotFeaturesWithATR(nil, gotValues, gotSignals, closes, 10, 1, atrValues, basic)
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("state UT Bot values differ: got=%#v want=%#v", gotValues, wantValues)
	}
	if !reflect.DeepEqual(gotSignals, wantSignals) {
		t.Fatalf("state UT Bot signals differ: got=%#v want=%#v", gotSignals, wantSignals)
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

func TestStreamQQEMatchesBatchAtEveryPoint(t *testing.T) {
	closes := oscillatingCloses(320)
	state := newStreamQQEState()
	for index := 1; index < len(closes); index++ {
		state.append(closes[index-1], closes[index])
		gotLine, gotSignal, gotPreviousLine, gotPreviousSignal, gotClassicOK := state.classicValue()
		wantLine, wantSignal, wantPreviousLine, wantPreviousSignal, wantClassicOK := qqeMod(closes[:index+1], 6, 5, 3)
		if gotClassicOK != wantClassicOK {
			t.Fatalf("index=%d classic stream ok = %v, want %v", index, gotClassicOK, wantClassicOK)
		}
		if gotClassicOK {
			assertFloatClose(t, "stream qqe line", gotLine, wantLine)
			assertFloatClose(t, "stream qqe signal", gotSignal, wantSignal)
			assertFloatClose(t, "stream qqe previous line", gotPreviousLine, wantPreviousLine)
			assertFloatClose(t, "stream qqe previous signal", gotPreviousSignal, wantPreviousSignal)
		}
		gotEnhanced, gotEnhancedOK := state.enhancedValue()
		wantEnhanced, wantEnhancedOK := qqeModEnhanced(closes[:index+1], 6, 5, 3, 1.61, 50, 0.35, 3)
		if gotEnhancedOK != wantEnhancedOK {
			t.Fatalf("index=%d enhanced stream ok = %v, want %v", index, gotEnhancedOK, wantEnhancedOK)
		}
		if gotEnhancedOK && gotEnhanced != wantEnhanced {
			t.Fatalf("index=%d enhanced stream = %#v, want %#v", index, gotEnhanced, wantEnhanced)
		}
	}
}

func TestQQEWithStateMatchesStandaloneFeatures(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(180, 100, 0.35)
	basic := buildBasicIndicatorState(highs, lows, closes, volumes)
	rsiValues, rsiOK := rsiSeries(closes, 6)
	smoothed, smoothedDeltas, offset, foundationOK := qqeModTrendFoundation(rsiValues, rsiOK, 6, 5)
	wantValues, wantSignals := map[string]string{}, map[string]string{}
	addQQEModFeaturesWithFoundation(nil, wantValues, wantSignals, smoothed, smoothedDeltas, 6, 5, 3, foundationOK)
	addQQEModEnhancedFeaturesWithFoundation(nil, wantValues, wantSignals, smoothed, smoothedDeltas, offset, foundationOK)
	gotValues, gotSignals := map[string]string{}, map[string]string{}
	if !addStreamQQEFeatures(nil, gotValues, gotSignals, basic.qqe6State()) {
		t.Fatal("missing stream QQE state")
	}
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("state QQE values differ: got=%#v want=%#v", gotValues, wantValues)
	}
	if !reflect.DeepEqual(gotSignals, wantSignals) {
		t.Fatalf("state QQE signals differ: got=%#v want=%#v", gotSignals, wantSignals)
	}
}

func TestQQEModEnhancedCompactMatchesReference(t *testing.T) {
	for _, count := range []int{120, 180, 268, 320} {
		closes := oscillatingCloses(count)
		rsiValues, rsiOK := rsiSeries(closes, 6)
		smoothed, smoothedDeltas, offset, foundationOK := qqeModTrendFoundation(rsiValues, rsiOK, 6, 5)

		got, gotOK := qqeModEnhancedFromFoundation(smoothed, smoothedDeltas, offset, 3, 1.61, 50, 0.35, 3, foundationOK)
		want, wantOK := qqeModEnhancedReference(smoothed, smoothedDeltas, offset, 3, 1.61, 50, 0.35, 3, foundationOK)

		if gotOK != wantOK || got != want {
			t.Fatalf("count %d: compact QQE = (%#v, %t), want (%#v, %t)", count, got, gotOK, want, wantOK)
		}
	}
}

func qqeModEnhancedReference(smoothed []float64, smoothedDeltas []float64, offset int, primaryFactor float64, secondaryFactor float64, bbPeriod int, bbMultiplier float64, secondaryThreshold float64, foundationOK bool) (qqeModEnhancedResult, bool) {
	if !foundationOK {
		return qqeModEnhancedResult{}, false
	}
	primaryTrend, primaryLine, okPrimary := qqeModTrendSeriesFromFoundation(smoothed, smoothedDeltas, offset, primaryFactor, true)
	secondaryTrend, _, okSecondary := qqeModTrendSeriesFromFoundation(smoothed, smoothedDeltas, offset, secondaryFactor, false)
	secondaryLine := primaryLine
	if !okPrimary || !okSecondary || len(primaryTrend) < bbPeriod || len(primaryLine) < 2 || len(secondaryLine) == 0 {
		return qqeModEnhancedResult{}, false
	}
	primaryTrendHist := make([]float64, 0, len(primaryTrend))
	for _, value := range primaryTrend {
		primaryTrendHist = append(primaryTrendHist, value-50)
	}
	basis, ok := sma(primaryTrendHist, bbPeriod)
	if !ok {
		return qqeModEnhancedResult{}, false
	}
	deviation, ok := standardDeviation(primaryTrendHist, bbPeriod)
	if !ok {
		return qqeModEnhancedResult{}, false
	}
	lastPrimary := primaryLine[len(primaryLine)-1]
	previousPrimary := primaryLine[len(primaryLine)-2]
	lastSecondary := secondaryLine[len(secondaryLine)-1]
	lastPrimaryHist := lastPrimary - 50
	lastSecondaryHist := lastSecondary - 50
	upper := basis + deviation*bbMultiplier
	lower := basis - deviation*bbMultiplier
	return qqeModEnhancedResult{
		primaryLine:    lastPrimary,
		primaryTrend:   primaryTrend[len(primaryTrend)-1],
		secondaryLine:  lastSecondary,
		secondaryTrend: secondaryTrend[len(secondaryTrend)-1],
		bbUpper:        upper,
		bbLower:        lower,
		primaryHist:    lastPrimaryHist,
		secondaryHist:  lastSecondaryHist,
		signal:         qqeModSignal(lastPrimaryHist, lastSecondaryHist, upper, lower, secondaryThreshold),
		zeroCross:      qqeZeroCross(previousPrimary, lastPrimary),
	}, true
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

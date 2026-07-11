package indicatorcalc

import (
	"reflect"
	"testing"
)

var benchmarkAISourceState *aiSourceState

func TestAISourceSwitchingSkipsShortSamples(t *testing.T) {
	opens, highs, lows, closes := aiSourceTestOHLC(60)
	values := map[string]string{}
	signals := map[string]string{}

	addAISourceSwitchingFeatures(values, signals, opens, highs, lows, closes)

	if values["ai_source_ma"] != "" {
		t.Fatalf("unexpected ai source values for short sample: %#v", values)
	}
	if signals["ai_source_ready"] != "false" {
		t.Fatalf("ai_source_ready = %q, want false", signals["ai_source_ready"])
	}
}

func TestAISourceSwitchingOutputsFields(t *testing.T) {
	opens, highs, lows, closes := aiSourceTestOHLC(260)
	values := map[string]string{}
	signals := map[string]string{}

	addAISourceSwitchingFeatures(values, signals, opens, highs, lows, closes)

	for _, key := range []string{
		"ai_source_ma",
		"ai_source_value",
		"ai_source_drive",
		"ai_source_score_open",
		"ai_source_score_high",
		"ai_source_score_low",
		"ai_source_score_close",
		"ai_source_supertrend",
		"ai_source_supertrend_distance_pct",
		"ai_source_supertrend_adapt_mult",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	for _, key := range []string{
		"ai_source_selected",
		"ai_source_changed",
		"ai_source_supertrend_direction",
		"ai_source_supertrend_flip",
		"ai_source_ready",
	} {
		if signals[key] == "" {
			t.Fatalf("missing %s in %#v", key, signals)
		}
	}
	if !signalIsAISourceName(signals["ai_source_selected"]) {
		t.Fatalf("unexpected selected source: %#v", signals)
	}
}

func TestAISourceBatchReferenceWindowsAreDeterministic(t *testing.T) {
	cfg := defaultAISourceConfig()
	for _, length := range []int{140, 250, 300} {
		opens, highs, lows, closes := aiSourceTestOHLC(length)
		first, firstOK := aiSourceSwitching(opens, highs, lows, closes, cfg)
		second, secondOK := aiSourceSwitching(opens, highs, lows, closes, cfg)
		if firstOK != secondOK || !reflect.DeepEqual(first, second) {
			t.Fatalf("length %d batch result is not deterministic: first=%#v/%v second=%#v/%v", length, first, firstOK, second, secondOK)
		}
		if !firstOK {
			t.Fatalf("length %d batch result was not available", length)
		}
	}
}

func TestAISourceBatchReferenceMatchesRepeatedRealtimeUpdates(t *testing.T) {
	cfg := defaultAISourceConfig()
	opens, highs, lows, closes := aiSourceTestOHLC(250)
	prefixOpens := append([]float64(nil), opens[:249]...)
	prefixHighs := append([]float64(nil), highs[:249]...)
	prefixLows := append([]float64(nil), lows[:249]...)
	prefixCloses := append([]float64(nil), closes[:249]...)

	for update := 0; update < 5; update++ {
		delta := float64(update) * 0.05
		previewOpens := append(append([]float64(nil), prefixOpens...), opens[249])
		previewHighs := append(append([]float64(nil), prefixHighs...), highs[249]+delta)
		previewLows := append(append([]float64(nil), prefixLows...), lows[249]-delta)
		previewCloses := append(append([]float64(nil), prefixCloses...), closes[249]+delta)
		first, firstOK := aiSourceSwitching(previewOpens, previewHighs, previewLows, previewCloses, cfg)
		second, secondOK := aiSourceSwitching(previewOpens, previewHighs, previewLows, previewCloses, cfg)
		if firstOK != secondOK || !reflect.DeepEqual(first, second) {
			t.Fatalf("update %d batch reference changed between runs: first=%#v/%v second=%#v/%v", update, first, firstOK, second, secondOK)
		}
		if !reflect.DeepEqual(prefixCloses, closes[:249]) {
			t.Fatalf("update %d mutated closed prefix", update)
		}
	}
}

func TestAISourceStateCloneIsIndependent(t *testing.T) {
	state := &aiSourceState{
		featureCursors:   [4]aiSourceFeatureCursor{newAISourceFeatureCursor([]float64{1, 2, 3})},
		banks:            [4][]aiSourceRow{{{outcome: 1}}, {{outcome: 2}}},
		allBank:          []aiSourceRow{{outcome: 3}},
		featureRing:      [][4][6]float64{{}},
		validFeatureRing: [][4]bool{{true}},
		sourceEMA:        newAISourceEMAState(3),
		maEMA:            newAISourceEMAState(5),
	}
	state.featureRing[0][0][0] = 1
	state.sourceEMA.append(10)
	state.maEMA.append(20)
	clone := state.clone()

	clone.featureCursors[0].source[0] = 99
	clone.banks[0][0].outcome = 9
	clone.allBank[0].outcome = 8
	clone.featureRing[0][0][0] = 7
	clone.validFeatureRing[0][0] = false
	clone.sourceEMA.append(30)
	clone.maEMA.append(40)

	if state.banks[0][0].outcome != 1 || state.allBank[0].outcome != 3 {
		t.Fatal("clone mutated source sample banks")
	}
	if state.featureCursors[0].source[0] != 1 {
		t.Fatal("clone mutated source feature cursor input")
	}
	if state.featureRing[0][0][0] != 1 || !state.validFeatureRing[0][0] {
		t.Fatal("clone mutated source feature ring")
	}
	if reflect.DeepEqual(state.sourceEMA, clone.sourceEMA) || reflect.DeepEqual(state.maEMA, clone.maEMA) {
		t.Fatal("clone shared or failed to advance independent EMA state")
	}
}

func TestAISourceStateAppendValidatesInputIndex(t *testing.T) {
	state := &aiSourceState{}
	input := aiSourceInput{closes: []float64{100}}
	calls := 0
	step := aiSourceStep(func(gotState *aiSourceState, gotInput aiSourceInput, index int) {
		calls++
		if gotState != state || index != 0 || gotInput.closes[0] != 100 {
			t.Fatalf("unexpected append arguments: state=%p input=%#v index=%d", gotState, gotInput, index)
		}
	})
	state.append(input, -1, step)
	state.append(input, 1, step)
	state.append(input, 0, nil)
	state.append(input, 0, step)
	if calls != 1 {
		t.Fatalf("step calls = %d, want 1 valid append", calls)
	}
}

func TestAISourceStatePrefixClonePlusOneMatchesFullBatchState(t *testing.T) {
	opens, highs, lows, closes := aiSourceTestOHLC(250)
	cfg := defaultAISourceConfig()
	atr14, ok := atrSeries(highs, lows, closes, 14)
	if !ok {
		t.Fatal("atr14 unavailable")
	}
	stATR, ok := atrSeries(highs, lows, closes, cfg.stLength)
	if !ok {
		t.Fatal("supertrend ATR unavailable")
	}
	input := aiSourceInput{
		sources: [4][]float64{opens, highs, lows, closes}, highs: highs, lows: lows, closes: closes,
		atr14: atr14, atr14Offset: len(closes) - len(atr14),
		stATR: stATR, stATROffset: len(closes) - len(stATR), config: cfg,
	}
	full := newAISourceState(input)
	for index := range closes {
		full.append(input, index, appendAISourceState)
	}
	prefix := newAISourceState(input)
	for index := 0; index < len(closes)-1; index++ {
		prefix.append(input, index, appendAISourceState)
	}
	preview := prefix.clone()
	preview.append(input, len(closes)-1, appendAISourceState)
	if !reflect.DeepEqual(preview, full) {
		t.Fatal("249-prefix clone plus one state differs from full 250-bar state")
	}
	if prefix.lineCount != len(closes)-1 {
		t.Fatalf("preview append mutated prefix line count: %d", prefix.lineCount)
	}
}

func TestAISourceFisherWeightsSeparateOutcomes(t *testing.T) {
	rows := []aiSourceRow{}
	for index := 0; index < 20; index++ {
		rows = append(rows, aiSourceRow{
			features: [6]float64{1, 0.2, 0.1, 0, 0, 0},
			outcome:  1,
		})
		rows = append(rows, aiSourceRow{
			features: [6]float64{-1, 0.2, 0.1, 0, 0, 0},
			outcome:  -1,
		})
	}

	weights := aiSourceFisherWeights(rows, 20, 0.4)

	if weights[0] <= weights[1] {
		t.Fatalf("trend feature should have higher fisher weight: %#v", weights)
	}
}

func TestAISourceFeaturesFromCacheMatchesBatch(t *testing.T) {
	_, highs, lows, closes := aiSourceTestOHLC(180)
	cache := newAISourceFeatureCache(closes)
	atrValues, ok := atrSeries(highs, lows, closes, 14)
	if !ok {
		t.Fatal("atrSeries returned false")
	}
	atrOffset := len(closes) - len(atrValues)

	for _, index := range []int{100, 120, 150, 179} {
		atrValue := atrValueAt(index, atrValues, atrOffset)
		got, ok := aiSourceFeaturesFromCache(cache, closes, highs, lows, index, atrValue)
		if !ok {
			t.Fatalf("aiSourceFeaturesFromCache(%d) returned false", index)
		}
		want, ok := aiSourceFeatures(closes, highs, lows, index, atrValue)
		if !ok {
			t.Fatalf("aiSourceFeatures(%d) returned false", index)
		}
		for featureIndex := range got {
			assertFloatClose(t, "ai source feature", got[featureIndex], want[featureIndex])
		}
	}
}

func TestAISourceFeatureCursorMatchesBatchCache(t *testing.T) {
	_, highs, lows, closes := aiSourceTestOHLC(180)
	cache := newAISourceFeatureCache(closes)
	cursor := newAISourceFeatureCursor(closes)
	atrValues, ok := atrSeries(highs, lows, closes, 14)
	if !ok {
		t.Fatal("atrSeries returned false")
	}
	atrOffset := len(closes) - len(atrValues)
	for index := range closes {
		gotPoint := cursor.next(index)
		wantPoint := cache.points[index]
		if gotPoint != wantPoint {
			t.Fatalf("point[%d] = %#v, want %#v", index, gotPoint, wantPoint)
		}
		atrValue := atrValueAt(index, atrValues, atrOffset)
		got, gotOK := aiSourceFeaturesFromPoint(gotPoint, closes, highs, lows, index, atrValue)
		want, wantOK := aiSourceFeaturesFromCache(cache, closes, highs, lows, index, atrValue)
		if gotOK != wantOK {
			t.Fatalf("features[%d] valid = %v, want %v", index, gotOK, wantOK)
		}
		if !gotOK {
			continue
		}
		for featureIndex := range got {
			assertFloatClose(t, "cursor feature", got[featureIndex], want[featureIndex])
		}
	}
}

func TestAISourceFeatureRingMatchesHistoricalRecalculation(t *testing.T) {
	opens, highs, lows, closes := aiSourceTestOHLC(180)
	sources := [][]float64{opens, highs, lows, closes}
	caches := [4]aiSourceFeatureCache{}
	for sourceID := range sources {
		caches[sourceID] = newAISourceFeatureCache(sources[sourceID])
	}
	atrValues, ok := atrSeries(highs, lows, closes, 14)
	if !ok {
		t.Fatal("atrSeries returned false")
	}
	atrOffset := len(closes) - len(atrValues)
	const horizon = 5
	featureRing := make([][4][6]float64, horizon+1)
	validRing := make([][4]bool, horizon+1)
	for index := range closes {
		atrValue := atrValueAt(index, atrValues, atrOffset)
		features := [4][6]float64{}
		valid := [4]bool{}
		for sourceID := range sources {
			features[sourceID], valid[sourceID] = aiSourceFeaturesFromCache(caches[sourceID], sources[sourceID], highs, lows, index, atrValue)
		}
		featureRing[index%(horizon+1)] = features
		validRing[index%(horizon+1)] = valid
		sampleIndex := index - horizon
		if sampleIndex < 0 {
			continue
		}
		for sourceID := range sources {
			want, wantOK := aiSourceFeaturesFromCache(caches[sourceID], sources[sourceID], highs, lows, sampleIndex, atrValueAt(sampleIndex, atrValues, atrOffset))
			got := featureRing[sampleIndex%(horizon+1)][sourceID]
			gotOK := validRing[sampleIndex%(horizon+1)][sourceID]
			if gotOK != wantOK {
				t.Fatalf("source=%d index=%d valid=%v want=%v", sourceID, sampleIndex, gotOK, wantOK)
			}
			for featureIndex := range got {
				assertFloatClose(t, "ring feature", got[featureIndex], want[featureIndex])
			}
		}
	}
}

func TestPrependAISourceRowReusesLimitedWindow(t *testing.T) {
	rows := make([]aiSourceRow, 0, 3)
	for _, outcome := range []int{1, 2, 3, 4} {
		rows = prependAISourceRow(rows, aiSourceRow{outcome: outcome}, 3)
	}

	if len(rows) != 3 {
		t.Fatalf("rows length = %d, want 3", len(rows))
	}
	for index, want := range []int{4, 3, 2} {
		if rows[index].outcome != want {
			t.Fatalf("rows[%d].outcome = %d, want %d", index, rows[index].outcome, want)
		}
	}
	if got := cap(rows); got != 3 {
		t.Fatalf("rows cap = %d, want 3", got)
	}

	rows = prependAISourceRow(rows, aiSourceRow{outcome: 5}, 0)
	if len(rows) != 0 {
		t.Fatalf("rows length after zero limit = %d, want 0", len(rows))
	}
}

func TestAISourceKNNScoreFixedMatchesBatch(t *testing.T) {
	cfg := defaultAISourceConfig()
	cfg.memoryDepth = 12
	cfg.kNeighbors = 5
	cfg.spacingBars = 1
	weights := [6]float64{1.2, 0.8, 1.5, 1, 0.7, 1.1}
	features := [6]float64{0.2, -0.1, 0.4, 0.1, -0.3, 0.5}
	bank := make([]aiSourceRow, 0, cfg.memoryDepth)
	for index := 0; index < cfg.memoryDepth; index++ {
		outcome := 1
		if index%3 == 0 {
			outcome = -1
		}
		bank = append(bank, aiSourceRow{
			features: [6]float64{
				float64(index%4) * 0.1,
				float64(index%5) * -0.08,
				float64(index%6) * 0.07,
				float64(index%3) * 0.05,
				float64(index%7) * -0.03,
				float64(index%4) * 0.09,
			},
			outcome: outcome,
		})
	}

	got := aiSourceKNNScore(features, bank, weights, cfg)
	want := aiSourceKNNScoreBatch(features, bank, weights, cfg)

	if got.count != want.count {
		t.Fatalf("knn count = %d, want %d", got.count, want.count)
	}
	assertFloatClose(t, "knn analog", got.analog, want.analog)
	assertFloatClose(t, "knn agree", got.agree, want.agree)
	assertFloatClose(t, "knn tight", got.tight, want.tight)
}

func TestAISourceEMAStateMatchesEMALast(t *testing.T) {
	values := linearValues(80, 100, 0.7)
	state := newAISourceEMAState(50)
	seen := make([]float64, 0, len(values))

	for _, value := range values {
		seen = append(seen, value)
		got := state.append(value)
		want := emaLast(seen, 50)
		assertFloatClose(t, "ai source ema", got, want)
	}
}

func signalIsAISourceName(value string) bool {
	return value == "open" || value == "high" || value == "low" || value == "close"
}

func aiSourceTestOHLC(length int) ([]float64, []float64, []float64, []float64) {
	opens := make([]float64, 0, length)
	highs := make([]float64, 0, length)
	lows := make([]float64, 0, length)
	closes := make([]float64, 0, length)
	price := 100.0
	for index := 0; index < length; index++ {
		if index%30 < 18 {
			price += 0.7
		} else {
			price -= 0.45
		}
		openValue := price - 0.2
		closeValue := price
		opens = append(opens, openValue)
		highs = append(highs, closeValue+1.2)
		lows = append(lows, openValue-1.1)
		closes = append(closes, closeValue)
	}
	return opens, highs, lows, closes
}

func BenchmarkAISourcePrefixClonePlusOne(b *testing.B) {
	opens, highs, lows, closes := aiSourceTestOHLC(250)
	cfg := defaultAISourceConfig()
	atr14, _ := atrSeries(highs, lows, closes, 14)
	stATR, _ := atrSeries(highs, lows, closes, cfg.stLength)
	input := aiSourceInput{
		sources: [4][]float64{opens, highs, lows, closes}, highs: highs, lows: lows, closes: closes,
		atr14: atr14, atr14Offset: len(closes) - len(atr14),
		stATR: stATR, stATROffset: len(closes) - len(stATR), config: cfg,
	}
	prefix := newAISourceState(input)
	for index := 0; index < len(closes)-1; index++ {
		prefix.append(input, index, appendAISourceState)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		preview := prefix.clone()
		preview.append(input, len(closes)-1, appendAISourceState)
		benchmarkAISourceState = preview
	}
}

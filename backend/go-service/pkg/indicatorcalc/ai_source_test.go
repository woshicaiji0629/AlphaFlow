package indicatorcalc

import "testing"

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

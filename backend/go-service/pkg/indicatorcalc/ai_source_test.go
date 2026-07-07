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

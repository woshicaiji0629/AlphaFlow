package indicatorcalc

import "testing"

func TestCandlePatternDetectsBullishEngulfing(t *testing.T) {
	signals := map[string]string{}
	opens := []float64{12, 9}
	highs := []float64{13, 13}
	lows := []float64{8, 8}
	closes := []float64{10, 12.5}

	addCandlePatterns(signals, opens, highs, lows, closes)

	if signals["candle_pattern"] != "bullish_engulfing" {
		t.Fatalf("candle_pattern = %q, want bullish_engulfing", signals["candle_pattern"])
	}
	if signals["candle_bias"] != "bull" {
		t.Fatalf("candle_bias = %q, want bull", signals["candle_bias"])
	}
	if signals["candle_strength"] != "medium" {
		t.Fatalf("candle_strength = %q, want medium", signals["candle_strength"])
	}
}

func TestCandlePatternDetectsBearishEngulfing(t *testing.T) {
	signals := map[string]string{}
	opens := []float64{9, 13}
	highs := []float64{13, 14}
	lows := []float64{8, 8}
	closes := []float64{12, 8.5}

	addCandlePatterns(signals, opens, highs, lows, closes)

	if signals["candle_pattern"] != "bearish_engulfing" {
		t.Fatalf("candle_pattern = %q, want bearish_engulfing", signals["candle_pattern"])
	}
	if signals["candle_bias"] != "bear" {
		t.Fatalf("candle_bias = %q, want bear", signals["candle_bias"])
	}
}

func TestCandlePatternDetectsMorningStar(t *testing.T) {
	signals := map[string]string{}
	opens := []float64{15, 10.5, 11}
	highs := []float64{16, 11, 15}
	lows := []float64{9, 10, 10.5}
	closes := []float64{10, 10.7, 14}

	addCandlePatterns(signals, opens, highs, lows, closes)

	if signals["candle_pattern"] != "morning_star" {
		t.Fatalf("candle_pattern = %q, want morning_star", signals["candle_pattern"])
	}
	if signals["candle_strength"] != "strong" {
		t.Fatalf("candle_strength = %q, want strong", signals["candle_strength"])
	}
}

func TestCandlePatternDetectsInsideBar(t *testing.T) {
	signals := map[string]string{}
	opens := []float64{10, 10.5}
	highs := []float64{15, 14}
	lows := []float64{8, 9}
	closes := []float64{14, 12}

	addCandlePatterns(signals, opens, highs, lows, closes)

	if signals["candle_pattern"] != "inside_bar" {
		t.Fatalf("candle_pattern = %q, want inside_bar", signals["candle_pattern"])
	}
}

func TestCandlePatternDetectsPinBar(t *testing.T) {
	signals := map[string]string{}
	opens := []float64{10}
	highs := []float64{10.5}
	lows := []float64{5}
	closes := []float64{10.2}

	addCandlePatterns(signals, opens, highs, lows, closes)

	if signals["candle_pattern"] != "hammer" {
		t.Fatalf("candle_pattern = %q, want hammer", signals["candle_pattern"])
	}
	if signals["pin_bar"] != "true" {
		t.Fatalf("pin_bar = %q, want true", signals["pin_bar"])
	}
}

func TestCandlePatternDetectsScriptTopFormation(t *testing.T) {
	signals := map[string]string{}
	opens := []float64{10, 11, 12, 13, 15, 14, 13, 12, 11, 12, 14, 13}
	highs := []float64{11, 12, 13, 14, 16, 15, 14, 13, 12, 14, 18, 15}
	lows := []float64{9, 10, 11, 12, 13, 12, 11, 10, 9, 11, 13, 10}
	closes := []float64{10.5, 11.5, 12.5, 13.5, 15, 13.5, 12.5, 11.5, 10.5, 13, 15, 12}

	addCandlePatterns(signals, opens, highs, lows, closes)

	if signals["candle_pattern"] != "top_formation" {
		t.Fatalf("candle_pattern = %q, want top_formation", signals["candle_pattern"])
	}
	if signals["candle_bias"] != "bear" {
		t.Fatalf("candle_bias = %q, want bear", signals["candle_bias"])
	}
}

func TestCandlePatternDetectsScriptRedThreeSoldiers(t *testing.T) {
	signals := map[string]string{}
	opens := []float64{10, 10.2, 10.8}
	highs := []float64{10.8, 11.4, 12.2}
	lows := []float64{9.8, 10, 10.6}
	closes := []float64{10.6, 11.2, 12}

	addCandlePatterns(signals, opens, highs, lows, closes)

	if signals["candle_pattern"] != "red_three_soldiers" {
		t.Fatalf("candle_pattern = %q, want red_three_soldiers", signals["candle_pattern"])
	}
	if signals["candle_strength"] != "strong" {
		t.Fatalf("candle_strength = %q, want strong", signals["candle_strength"])
	}
}

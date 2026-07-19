package indicatorwindow

import (
	"math"
	"strconv"
	"testing"
)

func TestMarketCapabilityV2SeparatesDirectionStrengthLocationAndRisk(t *testing.T) {
	bull := capabilityTestContext("bull")
	addMarketCapabilityScore(bull)
	bear := capabilityTestContext("bear")
	bear.signals["channel_position_state"] = "lower"
	bear.signals["volume_profile_window_position"] = "below_value_area"
	addMarketCapabilityScore(bear)

	if got := bull.signals["market_score_version"]; got != MarketCapabilityScoreVersion {
		t.Fatalf("score version = %q", got)
	}
	if numericOutput(t, bull, "market_direction_score") != 100 || numericOutput(t, bear, "market_direction_score") != -100 {
		t.Fatal("direction scores are not symmetric")
	}
	if numericOutput(t, bull, "market_location_score") != 100 || numericOutput(t, bear, "market_location_score") != -100 {
		t.Fatal("location scores are not directional")
	}
	if math.Abs(numericOutput(t, bull, "market_strength_score")-numericOutput(t, bear, "market_strength_score")) > 1e-9 {
		t.Fatal("direction changed direction-independent market strength")
	}
	if math.Abs(numericOutput(t, bull, "market_directional_capability_score")+numericOutput(t, bear, "market_directional_capability_score")) > 1e-9 {
		t.Fatal("directional capability is not symmetric")
	}
}

func TestMarketCapabilityV2RecognizesRealSemanticEnums(t *testing.T) {
	ctx := capabilityTestContext("bull")
	ctx.signals["ma_window_phase"] = "weakening"
	ctx.signals["momentum_window_level"] = "overbought"
	ctx.signals["channel_fake_risk"] = "medium"
	ctx.signals["exhaustion_risk"] = "high"
	ctx.signals["volume_profile_window_position"] = "inside_value_area"
	addMarketCapabilityScore(ctx)

	available := numericOutput(t, ctx, "market_score_available_features")
	expected := numericOutput(t, ctx, "market_score_expected_features")
	if available != expected {
		t.Fatalf("coverage available=%v expected=%v", available, expected)
	}
	if numericOutput(t, ctx, "market_data_confidence") != 100 {
		t.Fatal("real semantic enums did not produce full confidence")
	}
	if numericOutput(t, ctx, "market_risk_score") <= 0 {
		t.Fatal("categorical exhaustion risk was not recognized")
	}
}

func TestMarketCapabilityV22SeparatesRawStrengthFromRiskAdjustment(t *testing.T) {
	clean := capabilityTestContext("bull")
	addMarketCapabilityScore(clean)
	risky := capabilityTestContext("bull")
	risky.signals["trend_window_reversal_risk"] = "true"
	risky.signals["momentum_window_overheated"] = "true"
	risky.signals["exhaustion_risk"] = "high"
	risky.signals["volume_window_climax"] = "true"
	risky.signals["ma_window_tangled"] = "true"
	addMarketCapabilityScore(risky)

	if numericOutput(t, risky, "market_strength_score") != numericOutput(t, clean, "market_strength_score") {
		t.Fatal("risk changed independent raw market strength")
	}
	if numericOutput(t, risky, "market_risk_score") <= numericOutput(t, clean, "market_risk_score") {
		t.Fatal("risk score did not increase")
	}
	if numericOutput(t, risky, "market_risk_adjusted_strength_score") >= numericOutput(t, clean, "market_risk_adjusted_strength_score") {
		t.Fatal("risk did not reduce risk-adjusted strength")
	}
	if numericOutput(t, risky, "market_directional_capability_score") >= numericOutput(t, clean, "market_directional_capability_score") {
		t.Fatal("risk did not reduce directional capability")
	}
	if numericOutput(t, risky, "market_directional_capability_score") < 0 {
		t.Fatal("risk reversed directional capability")
	}
}

func TestMarketCapabilityV21TreatsFreshFormationAsStateNotFailure(t *testing.T) {
	ctx := capabilityTestContext("bull")
	ctx.signals["trend_window_continuation"] = "false"
	ctx.signals["trend_window_flip_recent"] = "true"
	ctx.signals["trend_window_reversal_risk"] = "true"
	ctx.signals["structure_window_event"] = "bull_choch"
	ctx.signals["smc_window_event"] = "bull_bos"
	addMarketCapabilityScore(ctx)

	if numericOutput(t, ctx, "market_trend_strength") <= 0 {
		t.Fatal("fresh trend formation was treated as zero strength")
	}
	if numericOutput(t, ctx, "market_structure_quality") <= 50 {
		t.Fatal("confirmed structure events were treated as structure failure")
	}
	if numericOutput(t, ctx, "market_risk_score") <= 0 {
		t.Fatal("fresh reversal risk disappeared from the independent risk score")
	}
}

func TestMarketCapabilityV2ReportsMissingCoverage(t *testing.T) {
	ctx := &analysisContext{values: map[string]string{}, signals: map[string]string{
		"trend_window_bias": "bull",
		"trend_quality":     "strong",
	}, encodeValues: true}
	addMarketCapabilityScore(ctx)

	if numericOutput(t, ctx, "market_score_available_features") != 2 {
		t.Fatal("partial input availability was not reported")
	}
	if numericOutput(t, ctx, "market_data_confidence") >= 75 {
		t.Fatal("partial input confidence unexpectedly sufficient")
	}
	if ctx.signals["market_strength_state"] != "insufficient" {
		t.Fatalf("strength state=%q", ctx.signals["market_strength_state"])
	}
}

func capabilityTestContext(direction string) *analysisContext {
	return &analysisContext{values: map[string]string{}, signals: map[string]string{
		"trend_window_bias":              direction,
		"ma_window_bias":                 direction,
		"macd_window_bias":               direction,
		"momentum_window_bias":           direction,
		"structure_window_bias":          direction,
		"money_flow_window_bias":         direction,
		"channel_window_bias":            direction,
		"trend_quality":                  "strong",
		"ma_window_phase":                "trend",
		"trend_window_continuation":      "true",
		"trend_window_flip_recent":       "false",
		"trend_price_progress":           "advancing",
		"trend_window_reversal_risk":     "false",
		"trend_fake_risk":                "low",
		"macd_window_quality":            "strong",
		"momentum_window_level":          "neutral",
		"price_volume_window_confirmed":  "true",
		"momentum_window_fading":         "false",
		"momentum_window_overheated":     "false",
		"volatility_window_state":        "normal",
		"channel_volatility_state":       "expanding",
		"channel_fake_risk":              "low",
		"structure_window_reversal_risk": "false",
		"smc_window_reversal_risk":       "false",
		"smc_window_bias":                direction,
		"structure_window_event":         "none",
		"smc_window_event":               "none",
		"volume_window_state":            "normal",
		"volume_window_climax":           "false",
		"channel_position_state":         "upper",
		"volume_profile_window_position": "above_value_area",
		"exhaustion_risk":                "low",
		"ma_window_tangled":              "false",
	}, encodeValues: true}
}

func numericOutput(t *testing.T, ctx *analysisContext, key string) float64 {
	t.Helper()
	value, ok := ctx.values[key]
	if !ok {
		t.Fatalf("numeric output %q missing", key)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		t.Fatalf("numeric output %q=%q is invalid", key, value)
	}
	return parsed
}

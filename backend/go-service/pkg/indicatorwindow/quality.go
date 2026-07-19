package indicatorwindow

import "math"

const MarketCapabilityScoreVersion = "market-capability.v2.7"

type capabilityDomain struct {
	total     float64
	available int
	expected  int
}

func (d *capabilityDomain) add(value float64, ok bool) {
	d.expected++
	if !ok {
		return
	}
	d.total += value
	d.available++
}

func (d capabilityDomain) score() float64 {
	if d.available == 0 {
		return 0
	}
	return d.total / float64(d.available)
}

type directionDomain struct {
	capabilityDomain
	positive int
	negative int
	neutral  int
}

func (d *directionDomain) addDirection(value string) {
	score, ok := directionalValue(value)
	d.add(score, ok)
	if !ok {
		return
	}
	switch {
	case score > 0:
		d.positive++
	case score < 0:
		d.negative++
	default:
		d.neutral++
	}
}

func (d directionDomain) agreement() float64 {
	if d.available == 0 {
		return 0
	}
	dominant := max(d.positive, d.negative)
	return float64(dominant) / float64(d.available) * 100
}

func (d directionDomain) conflict() float64 {
	directional := d.positive + d.negative
	if directional == 0 {
		return 0
	}
	return float64(min(d.positive, d.negative)) / float64(directional) * 200
}

// addMarketCapabilityScore describes the current market independently from a
// strategy. Direction, environment strength, position, and risk remain
// separate so consumers can apply their own candidate-side semantics.
func addMarketCapabilityScore(ctx *analysisContext) {
	direction := directionDomain{}
	for _, key := range []string{
		"trend_window_bias",
		"ma_window_bias",
		"macd_window_bias",
		"momentum_window_bias",
		"structure_window_bias",
		"money_flow_window_bias",
		"channel_window_bias",
	} {
		direction.addDirection(ctx.signals[key])
	}

	trend := capabilityDomain{}
	value, ok := qualityValue(ctx.signals["trend_quality"])
	trend.add(value, ok)
	value, ok = phaseStrengthValue(ctx.signals["ma_window_phase"])
	trend.add(value, ok)
	value, ok = trendFormationValue(
		ctx.signals["trend_window_bias"],
		ctx.signals["trend_window_continuation"],
		ctx.signals["trend_window_flip_recent"],
		ctx.signals["trend_price_progress"],
	)
	trend.add(value, ok)

	momentum := capabilityDomain{}
	value, ok = qualityValue(ctx.signals["macd_window_quality"])
	momentum.add(value, ok)
	value, ok = momentumStateValue(ctx.signals["momentum_window_level"])
	momentum.add(value, ok)
	value, ok = positiveWithRiskValue(ctx.signals["price_volume_window_confirmed"], ctx.signals["momentum_window_fading"])
	momentum.add(value, ok)

	volatility := capabilityDomain{}
	value, ok = volatilityHealthValue(ctx.signals["volatility_window_state"])
	volatility.add(value, ok)
	value, ok = volatilityHealthValue(ctx.signals["channel_volatility_state"])
	volatility.add(value, ok)
	value, ok = categoricalSafetyValue(ctx.signals["channel_fake_risk"])
	volatility.add(value, ok)

	structure := capabilityDomain{}
	value, ok = structureBiasClarityValue(ctx.signals["structure_window_bias"], ctx.signals["smc_window_bias"])
	structure.add(value, ok)
	value, ok = structureEventClarityValue(ctx.signals["structure_window_event"])
	structure.add(value, ok)
	value, ok = structureEventClarityValue(ctx.signals["smc_window_event"])
	structure.add(value, ok)

	volume := capabilityDomain{}
	value, ok = booleanStrengthValue(ctx.signals["price_volume_window_confirmed"])
	volume.add(value, ok)
	value, ok = volumeStateValue(ctx.signals["volume_window_state"])
	volume.add(value, ok)

	location := capabilityDomain{}
	value, ok = channelLocationValue(ctx.signals["channel_position_state"])
	location.add(value, ok)
	value, ok = profileLocationValue(ctx.signals["volume_profile_window_position"])
	location.add(value, ok)

	risk := capabilityDomain{}
	value, ok = booleanRiskValue(ctx.signals["trend_window_reversal_risk"], 100)
	risk.add(value, ok)
	value, ok = booleanRiskValue(ctx.signals["momentum_window_fading"], 75)
	risk.add(value, ok)
	value, ok = booleanRiskValue(ctx.signals["momentum_window_overheated"], 75)
	risk.add(value, ok)
	value, ok = categoricalRiskValue(ctx.signals["exhaustion_risk"])
	risk.add(value, ok)
	value, ok = booleanRiskValue(ctx.signals["volume_window_climax"], 75)
	risk.add(value, ok)
	value, ok = booleanRiskValue(ctx.signals["ma_window_tangled"], 100)
	risk.add(value, ok)

	available := direction.available + trend.available + momentum.available + volatility.available +
		structure.available + volume.available + location.available + risk.available
	expected := direction.expected + trend.expected + momentum.expected + volatility.expected +
		structure.expected + volume.expected + location.expected + risk.expected
	confidence := coverageScore(available, expected)
	strength := averageAvailableDomains(trend, momentum, volatility, structure, volume)
	riskScore := risk.score()
	riskAdjustedStrength := clamp(strength-riskScore, 0, 100)
	directionalEvidence := direction.score()*0.8 + location.score()*0.2
	directionalCapability := clamp(directionalEvidence*(riskAdjustedStrength/100)*(confidence/100), -100, 100)

	ctx.setNumericValue("market_direction_score", direction.score(), true)
	ctx.setNumericValue("market_direction_agreement", direction.agreement(), true)
	ctx.setNumericValue("market_direction_conflict", direction.conflict(), true)
	ctx.setNumericValue("market_trend_strength", trend.score(), true)
	ctx.setNumericValue("market_momentum_strength", momentum.score(), true)
	ctx.setNumericValue("market_volatility_health", volatility.score(), true)
	ctx.setNumericValue("market_structure_quality", structure.score(), true)
	ctx.setNumericValue("market_volume_confirmation", volume.score(), true)
	ctx.setNumericValue("market_location_score", location.score(), true)
	ctx.setNumericValue("market_risk_score", riskScore, true)
	ctx.setNumericValue("market_data_confidence", confidence, true)
	ctx.setNumericValue("market_score_available_features", float64(available), true)
	ctx.setNumericValue("market_score_expected_features", float64(expected), true)
	ctx.setNumericValue("market_strength_score", strength, true)
	ctx.setNumericValue("market_risk_adjusted_strength_score", riskAdjustedStrength, true)
	ctx.setNumericValue("market_directional_capability_score", directionalCapability, true)
	ctx.signals["market_score_version"] = MarketCapabilityScoreVersion
	ctx.signals["market_direction_bias"] = scoreBias(direction.score())
	ctx.signals["market_strength_state"] = strengthState(strength, confidence)
}

func averageAvailableDomains(domains ...capabilityDomain) float64 {
	total := 0.0
	count := 0
	for _, domain := range domains {
		if domain.available == 0 {
			continue
		}
		total += domain.score()
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func coverageScore(available int, expected int) float64 {
	if expected == 0 {
		return 0
	}
	return float64(available) / float64(expected) * 100
}

func directionalValue(value string) (float64, bool) {
	switch normalizeSignal(value) {
	case "bull", "bullish", "up", "buy", "long", "positive":
		return 100, true
	case "bear", "bearish", "down", "sell", "short", "negative":
		return -100, true
	case "neutral", "flat", "none", "mixed":
		return 0, true
	default:
		return 0, false
	}
}

func qualityValue(value string) (float64, bool) {
	switch normalizeSignal(value) {
	case "strong", "confirmed":
		return 100, true
	case "normal", "neutral":
		return 60, true
	case "weak":
		return 35, true
	case "choppy", "invalid", "blocked":
		return 0, true
	default:
		return 0, false
	}
}

func phaseStrengthValue(value string) (float64, bool) {
	switch normalizeSignal(value) {
	case "trend", "spreading":
		return 100, true
	case "early_cross":
		return 70, true
	case "neutral", "pullback":
		return 50, true
	case "weakening":
		return 25, true
	case "choppy", "tangled", "flat":
		return 0, true
	default:
		return 0, false
	}
}

func trendFormationValue(bias string, continuation string, flipRecent string, priceProgress string) (float64, bool) {
	direction, directionOK := directionalValue(bias)
	continuing, continuationOK := booleanValue(continuation)
	forming, flipOK := booleanValue(flipRecent)
	if !directionOK || (!continuationOK && !flipOK) {
		return 0, false
	}
	if direction == 0 {
		return 0, true
	}
	switch {
	case continuing > 0 && signalIs(priceProgress, "advancing"):
		return 100, true
	case continuing > 0:
		return 80, true
	case forming > 0 && signalIs(priceProgress, "advancing"):
		return 75, true
	case forming > 0:
		return 60, true
	case signalIs(priceProgress, "advancing"):
		return 55, true
	default:
		return 35, true
	}
}

func structureBiasClarityValue(primary string, secondary string) (float64, bool) {
	primaryValue, primaryOK := directionalValue(primary)
	secondaryValue, secondaryOK := directionalValue(secondary)
	if !primaryOK && !secondaryOK {
		return 0, false
	}
	if !primaryOK || primaryValue == 0 {
		if secondaryOK && secondaryValue != 0 {
			return 65, true
		}
		return 30, true
	}
	if !secondaryOK || secondaryValue == 0 {
		return 65, true
	}
	if primaryValue == secondaryValue {
		return 100, true
	}
	return 0, true
}

func structureEventClarityValue(value string) (float64, bool) {
	normalized := normalizeSignal(value)
	switch {
	case signalIs(normalized, "none", "neutral", "no"):
		return 50, true
	case signalContains(normalized, "bos", "breakout", "breakdown", "break"):
		return 100, true
	case signalContains(normalized, "choch"):
		return 70, true
	case signalContains(normalized, "sweep", "retest"):
		return 60, true
	default:
		return 0, false
	}
}

func momentumStateValue(value string) (float64, bool) {
	switch normalizeSignal(value) {
	case "strong", "high", "expanding":
		return 100, true
	case "normal", "medium", "neutral":
		return 60, true
	case "overbought", "oversold":
		return 40, true
	case "weak", "low", "fading", "flat":
		return 20, true
	default:
		return 0, false
	}
}

func volatilityHealthValue(value string) (float64, bool) {
	switch normalizeSignal(value) {
	case "normal", "expanding":
		return 100, true
	case "contracting", "squeeze", "low", "flat", "neutral":
		return 50, true
	case "climax", "extreme", "shock", "abnormal":
		return 10, true
	default:
		return 0, false
	}
}

func channelLocationValue(value string) (float64, bool) {
	switch normalizeSignal(value) {
	case "upper", "above":
		return 100, true
	case "lower", "below":
		return -100, true
	case "middle", "inside", "neutral", "unknown":
		return 0, true
	default:
		return 0, false
	}
}

func profileLocationValue(value string) (float64, bool) {
	switch normalizeSignal(value) {
	case "above_value_area", "upper_breakout":
		return 100, true
	case "below_value_area", "lower_breakdown":
		return -100, true
	case "inside_value_area", "unknown", "neutral":
		return 0, true
	default:
		return 0, false
	}
}

func volumeStateValue(value string) (float64, bool) {
	switch normalizeSignal(value) {
	case "expansion":
		return 100, true
	case "normal":
		return 60, true
	case "dry":
		return 30, true
	case "climax":
		return 20, true
	default:
		return 0, false
	}
}

func positiveWithRiskValue(positive string, risk string) (float64, bool) {
	positiveValue, positiveOK := booleanValue(positive)
	riskValue, riskOK := booleanValue(risk)
	if !positiveOK && !riskOK {
		return 0, false
	}
	return clamp(50+positiveValue*50-riskValue*50, 0, 100), true
}

func booleanStrengthValue(value string) (float64, bool) {
	set, ok := booleanValue(value)
	if !ok {
		return 0, false
	}
	return set * 100, true
}

func booleanSafetyValue(value string) (float64, bool) {
	set, ok := booleanValue(value)
	if !ok {
		return 0, false
	}
	return 100 - set*100, true
}

func categoricalSafetyValue(value string) (float64, bool) {
	risk, ok := categoricalRiskValue(value)
	if !ok {
		return 0, false
	}
	return 100 - risk, true
}

func categoricalRiskValue(value string) (float64, bool) {
	switch normalizeSignal(value) {
	case "low", "false", "none":
		return 0, true
	case "medium", "moderate":
		return 50, true
	case "high", "true":
		return 100, true
	default:
		return 0, false
	}
}

func booleanRiskValue(value string, amount float64) (float64, bool) {
	set, ok := booleanValue(value)
	if !ok {
		return 0, false
	}
	return set * amount, true
}

func booleanValue(value string) (float64, bool) {
	switch normalizeSignal(value) {
	case "true", "yes", "1":
		return 1, true
	case "false", "no", "0":
		return 0, true
	default:
		return 0, false
	}
}

func scoreBias(score float64) string {
	switch {
	case score >= 20:
		return "bull"
	case score <= -20:
		return "bear"
	default:
		return "neutral"
	}
}

func strengthState(strength float64, confidence float64) string {
	if confidence < 75 {
		return "insufficient"
	}
	switch {
	case strength >= 70:
		return "strong"
	case strength >= 40:
		return "normal"
	default:
		return "weak"
	}
}

func clamp(value float64, minimum float64, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

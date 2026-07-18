package supertrend

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

const (
	Name                            = "supertrend"
	ExitModeStructure               = "structure"
	ExitModeTrailing                = "trailing"
	ExitModeAdaptive                = "adaptive"
	DefaultProfitGuardActivationBps = 150.0
	DefaultProfitGuardFloorBps      = 100.0
	DefaultProfitDecayActivationBps = 150.0
	DefaultAdaptiveMaxStopLossBps   = 70.0
	DefaultRoundTripCostBps         = 16.0
	DefaultProfitBufferBps          = 8.0
	DefaultMicroProfitQuote         = 10.0
	DefaultTargetProfitQuote        = 20.0
	DefaultRunnerProfitQuote        = 30.0

	adaptiveLowTrailATRMultiplier       = 0.75
	adaptiveNormalTrailATRMultiplier    = 1.0
	adaptiveExpandingTrailATRMultiplier = 1.25
	adaptiveRunnerTrailMultiplier       = 1.75
	adaptiveMinLowTrailPct              = 0.18
	adaptiveMaxLowTrailPct              = 0.35
	adaptiveMinNormalTrailPct           = 0.25
	adaptiveMaxNormalTrailPct           = 0.55
	adaptiveMinExpandingTrailPct        = 0.35
	adaptiveMaxExpandingTrailPct        = 0.75
	adaptiveMaxRunnerTrailPct           = 1.20
)

type Config struct {
	EntryThreshold           float64
	MaxBlockingTimeframes    int
	MinTakeProfitBps         float64
	TakeProfitCostFloorBps   float64
	MinRewardRiskRatio       float64
	MaxStopLossBps           float64
	ExitMode                 string
	TrailingStopPct          float64
	ProfitGuardActivationBps float64
	ProfitGuardFloorBps      float64
	ProfitDecayActivationBps float64
	RoundTripCostBps         float64
	ProfitBufferBps          float64
	MicroProfitQuote         float64
	TargetProfitQuote        float64
	RunnerProfitQuote        float64
}

type Strategy struct {
	config Config
}

type entryDecision struct {
	side    strategy.SignalSide
	score   float64
	blocked bool
	reasons []string
	checks  []strategy.DiagnosticCheck
}

type adaptiveExitParameters struct {
	volatilityState     string
	atrPct              float64
	activationBps       float64
	floorBps            float64
	decayActivationBps  float64
	runnerActivationBps float64
	trailPct            float64
	runnerTrailPct      float64
}

func New(config Config) *Strategy {
	if config.EntryThreshold <= 0 {
		config.EntryThreshold = 0.72
	}
	if config.MaxBlockingTimeframes <= 0 {
		config.MaxBlockingTimeframes = 1
	}
	config.ExitMode = normalize(config.ExitMode)
	if config.ExitMode == "" {
		config.ExitMode = ExitModeStructure
	}
	if config.ExitMode == ExitModeTrailing {
		if config.ProfitGuardActivationBps <= 0 {
			config.ProfitGuardActivationBps = DefaultProfitGuardActivationBps
		}
		if config.ProfitGuardFloorBps <= 0 {
			config.ProfitGuardFloorBps = DefaultProfitGuardFloorBps
		}
		if config.ProfitDecayActivationBps <= 0 {
			config.ProfitDecayActivationBps = DefaultProfitDecayActivationBps
		}
	} else if config.ExitMode == ExitModeAdaptive {
		if config.MaxStopLossBps <= 0 {
			config.MaxStopLossBps = DefaultAdaptiveMaxStopLossBps
		}
		if config.RoundTripCostBps <= 0 {
			config.RoundTripCostBps = DefaultRoundTripCostBps
		}
		if config.ProfitBufferBps <= 0 {
			config.ProfitBufferBps = DefaultProfitBufferBps
		}
		if config.MicroProfitQuote <= 0 {
			config.MicroProfitQuote = DefaultMicroProfitQuote
		}
		if config.TargetProfitQuote <= 0 {
			config.TargetProfitQuote = DefaultTargetProfitQuote
		}
		if config.RunnerProfitQuote <= 0 {
			config.RunnerProfitQuote = DefaultRunnerProfitQuote
		}
	}
	return &Strategy{config: config}
}

func (s *Strategy) Name() string {
	return Name
}

func (s *Strategy) Requirements(target strategy.Target) strategy.Requirements {
	return strategy.Requirements{
		EntryInterval:    target.Interval,
		ConfirmIntervals: []string{"5m", "10m", "15m", "30m"},
		Trigger:          strategy.TriggerOnEntryClose,
	}
}

func (s *Strategy) Evaluate(
	ctx context.Context,
	snapshot strategy.Snapshot,
	currentPosition *strategy.Position,
) (strategy.Result, error) {
	if err := ctx.Err(); err != nil {
		return strategy.Result{}, err
	}
	if !snapshot.Health.OK {
		return s.hold(snapshot, "snapshot health not ok"), nil
	}
	if !dataQualityOK(snapshot.Window) {
		return s.hold(snapshot, "data quality not ok"), nil
	}
	if currentPosition != nil && !currentPosition.IsFlat() {
		return s.evaluateExit(snapshot, currentPosition), nil
	}

	longDecision := s.entry(snapshot, strategy.SignalSideBuy)
	shortDecision := s.entry(snapshot, strategy.SignalSideSell)
	longDecision = withThresholdCheck(longDecision, s.config.EntryThreshold)
	shortDecision = withThresholdCheck(shortDecision, s.config.EntryThreshold)
	checks := make([]strategy.DiagnosticCheck, 0, len(longDecision.checks)+len(shortDecision.checks))
	checks = append(checks, longDecision.checks...)
	checks = append(checks, shortDecision.checks...)
	selected := selectEntry(longDecision, shortDecision, s.config.EntryThreshold)
	if selected.side == strategy.SignalSideHold {
		return s.holdWithChecks(snapshot, strings.Join(append(longDecision.reasons, shortDecision.reasons...), "; "), checks), nil
	}
	rules := s.exitRules(snapshot, selected.side)
	if (s.config.ExitMode == ExitModeTrailing || s.config.ExitMode == ExitModeAdaptive) && !hasExitRule(rules, strategy.ExitReasonTrailingStop) {
		check := diagnosticCheck("exit_rules", selected.side, false, 0, "trailing stop reference price missing", map[string]string{
			"exit_mode":         s.config.ExitMode,
			"trailing_stop_pct": formatFloat(s.config.TrailingStopPct),
		})
		checks = append(checks, check)
		return s.holdWithChecks(snapshot, check.Reason, checks), nil
	}
	if s.exitGeometryGuardEnabled() {
		check := s.exitGeometryCheck(snapshot, selected.side, rules)
		checks = append(checks, check)
		if check.Status != strategy.DiagnosticStatusPass {
			return s.holdWithChecks(snapshot, check.Reason, checks), nil
		}
	}
	return s.signalWithChecks(snapshot, selected.side, selected.score, strings.Join(selected.reasons, "; "), rules, checks), nil
}

func (s *Strategy) evaluateExit(snapshot strategy.Snapshot, currentPosition *strategy.Position) strategy.Result {
	var opposite strategy.SignalSide
	var holdingSide strategy.SignalSide
	switch currentPosition.Side {
	case strategy.PositionSideLong:
		holdingSide = strategy.SignalSideBuy
		opposite = strategy.SignalSideSell
	case strategy.PositionSideShort:
		holdingSide = strategy.SignalSideSell
		opposite = strategy.SignalSideBuy
	default:
		return s.hold(snapshot, "position side not actionable")
	}
	stcCheck := stcObservationCheck(snapshot.Window, holdingSide)
	exitContextChecks := []strategy.DiagnosticCheck{stcCheck}
	if s.config.ExitMode == ExitModeAdaptive {
		invalidationCheck, invalidated := adaptiveDirectionInvalidated(snapshot, currentPosition, holdingSide)
		if invalidated {
			invalidationCheck.Status = strategy.DiagnosticStatusInfo
			invalidationCheck.Reason = "intraday direction invalidated; background only"
		}
		exitContextChecks = append(exitContextChecks, invalidationCheck)
	}
	if reversed, values := higherTimeframeReversed(snapshot, opposite); reversed {
		exitContextChecks = append(exitContextChecks, infoDiagnostic("structure_invalidation", opposite, "10m and 15m reversal observed; background only", values))
	}
	profitCheck, _ := s.profitProtectionCheck(snapshot, currentPosition, holdingSide)
	exitContextChecks = append(exitContextChecks, profitCheck)
	decision := s.reversalEntry(snapshot, opposite)
	decision = withThresholdCheck(decision, s.config.EntryThreshold)
	if decision.blocked {
		return s.holdWithChecks(snapshot, strings.Join(decision.reasons, "; "), append(decision.checks, exitContextChecks...))
	}
	if !reverseConfirmed(snapshot, opposite) {
		checks := append(decision.checks, diagnosticCheck("reverse_confirmation", opposite, false, 0, "reverse signal not confirmed", nil))
		checks = append(checks, exitContextChecks...)
		return s.holdWithChecks(snapshot, "reverse signal not confirmed", checks)
	}
	checks := append(decision.checks, diagnosticCheck("reverse_confirmation", opposite, true, 0, "reverse signal confirmed", nil))
	checks = append(checks, exitContextChecks...)
	return s.signalWithChecks(snapshot, opposite, decision.score, strings.Join(decision.reasons, "; "), nil, checks)
}

func (s *Strategy) entry(snapshot strategy.Snapshot, side strategy.SignalSide) entryDecision {
	return s.evaluateEntry(snapshot, side)
}

func (s *Strategy) reversalEntry(snapshot strategy.Snapshot, side strategy.SignalSide) entryDecision {
	return s.evaluateEntry(snapshot, side)
}

func (s *Strategy) evaluateEntry(snapshot strategy.Snapshot, side strategy.SignalSide) entryDecision {
	window := snapshot.Window
	decision := entryDecision{
		side:    side,
		reasons: []string{},
	}
	if window.Empty() {
		decision.blocked = true
		decision.reasons = append(decision.reasons, "indicator window missing")
		decision.checks = append(decision.checks, strategy.DiagnosticCheck{Name: "indicator_window", Side: side, Status: strategy.DiagnosticStatusMissing, Reason: "indicator window missing"})
		return decision
	}
	triggerSources := standardEntryTriggerSources(window, side)
	supertrendSources := supertrendEntryTriggerSources(triggerSources)
	triggerValues := entryTriggerValues(window, side, triggerSources)
	standardTriggered := len(supertrendSources) > 0
	triggerValues["standard_trigger_authorized"] = strconv.FormatBool(standardTriggered)
	triggerValues["supertrend_trigger_sources"] = strings.Join(supertrendSources, ",")
	triggerValues["supertrend_trigger_count"] = strconv.Itoa(len(supertrendSources))
	if !standardTriggered {
		decision.blocked = true
		reason := fmt.Sprintf("%s supertrend trigger missing", side)
		if len(triggerSources) > 0 {
			reason = fmt.Sprintf("%s auxiliary trigger cannot authorize entry", side)
		}
		decision.reasons = append(decision.reasons, reason)
		decision.checks = append(decision.checks, diagnosticCheck("entry_trigger", side, false, 0, reason, triggerValues))
		return decision
	}
	decision.reasons = append(decision.reasons, fmt.Sprintf("%s trigger", side))
	decision.checks = append(decision.checks, diagnosticCheck("entry_trigger", side, true, 0, fmt.Sprintf("%s trigger", side), triggerValues))

	energyOK, _, energyReasons, energyValues := momentumEnergy(window, side)
	fakeRisk := fakeRiskLevel(window, side)
	regimeOK, regimeValues := higherTimeframeAuthorized(snapshot, side)
	pullbackOK, pullbackValues := pullbackResolved(snapshot, side)
	aligned, blocked := classifyTimeframes(snapshot, side)
	entryMode := "supertrend_signal"
	decision.reasons = append(decision.reasons, "supertrend signal authorized")
	modeValues := map[string]string{
		"mode":                       entryMode,
		"trigger_sources":            strings.Join(supertrendSources, ","),
		"trigger_source_count":       strconv.Itoa(len(supertrendSources)),
		"ma_tangled":                 strconv.FormatBool(truthySignal(window, "ma_window_tangled")),
		"volatility_state":           latestSignal(window, "volatility_window_state"),
		"local_supertrend_direction": latestSignal(window, "supertrend_direction"),
		"local_trend_bias":           latestSignal(window, "trend_window_bias"),
		"local_ma_bias":              latestSignal(window, "ma_window_bias"),
		"local_macd_bias":            latestSignal(window, "macd_window_bias"),
		"regime_authorized":          strconv.FormatBool(regimeOK),
		"pullback_resolved":          strconv.FormatBool(pullbackOK),
	}
	decision.checks = append(decision.checks, diagnosticCheck("entry_mode", side, true, 0, entryMode+" authorized", modeValues))
	decision.checks = append(decision.checks, entryFeatureSnapshotCheck(window, side))
	stcCheck, stcVetoed := stcEntryCheck(window, side)
	decision.checks = append(decision.checks, stcCheck)
	if stcVetoed {
		decision.blocked = true
		decision.reasons = append(decision.reasons, "stc 25-line entry veto")
	}

	fakeBlocked := fakeRisk == "high"
	fakeAccepted := !fakeBlocked
	fakeReason := "fake signal risk accepted"
	if !fakeAccepted {
		fakeReason = "high fake signal risk observed"
	}
	decision.checks = append(decision.checks, entryConfirmationCheck("fake_signal_risk", side, fakeAccepted, false, 0, fakeReason, map[string]string{
		"risk": fakeRisk,
	}))

	regimeAccepted := regimeOK
	regimeCheckValues := regimeValues
	regimeReason := "higher timeframe regime authorized"
	if !regimeAccepted {
		regimeReason = "higher timeframe opposition observed"
	}
	decision.checks = append(decision.checks, entryConfirmationCheck("higher_timeframe_regime", side, regimeAccepted, false, 0, regimeReason, regimeCheckValues))

	pullbackReason := "5m direction aligned"
	if !pullbackOK {
		pullbackReason = "5m direction is background only"
	}
	decision.checks = append(decision.checks, entryConfirmationCheck("pullback_resolution", side, pullbackOK, false, 0, pullbackReason, pullbackValues))

	trendOK, _, trendReasons := trendConfirmationForEntry(window, side)
	decision.reasons = append(decision.reasons, trendReasons...)
	decision.checks = append(decision.checks, entryConfirmationCheck("trend", side, trendOK, false, 0, strings.Join(trendReasons, "; "), nil))

	maOK, _, maReasons := maConfirmationForEntry(window, side)
	decision.reasons = append(decision.reasons, maReasons...)
	decision.checks = append(decision.checks, entryConfirmationCheck("moving_average", side, maOK, false, 0, strings.Join(maReasons, "; "), nil))

	macdOK, _, macdReasons := macdConfirmation(window, side)
	decision.reasons = append(decision.reasons, macdReasons...)
	decision.checks = append(decision.checks, entryConfirmationCheck("macd", side, macdOK, false, 0, strings.Join(macdReasons, "; "), nil))

	volumeOK, _, volumeReasons := volumeConfirmation(window, side)
	decision.reasons = append(decision.reasons, volumeReasons...)
	decision.checks = append(decision.checks, entryConfirmationCheck("volume", side, volumeOK, false, 0, strings.Join(volumeReasons, "; "), nil))

	decision.reasons = append(decision.reasons, energyReasons...)
	decision.checks = append(decision.checks, entryConfirmationCheck("momentum_energy", side, energyOK, false, 0, strings.Join(energyReasons, "; "), energyValues))

	shortBlocked := shortTimeframesBlocked(snapshot, side)
	timeframesOK := blocked <= s.config.MaxBlockingTimeframes && !shortBlocked
	decision.checks = append(decision.checks, entryConfirmationCheck("timeframes", side, timeframesOK, false, 0, checkReason(!timeframesOK, "timeframe opposition observed", "timeframes evaluated"), map[string]string{
		"aligned": strconv.Itoa(aligned), "blocked": strconv.Itoa(blocked),
	}))
	qualityScore, qualityValues := entryQualityScore(regimeValues["state"], pullbackValues["5m"], energyValues["confirmations"], modeValues["volatility_state"])
	decision.score = qualityScore
	decision.checks = append(decision.checks, diagnosticCheck("entry_quality", side, true, decision.score, "entry quality scored", qualityValues))
	return decision
}

func entryFeatureSnapshotCheck(window strategy.IndicatorWindowView, side strategy.SignalSide) strategy.DiagnosticCheck {
	values := map[string]string{}
	for _, key := range []string{
		"supertrend_distance_pct",
		"supertrend_stop_distance_pct",
		"supertrend_zone_position_pct",
		"atr_pct14",
		"adx14",
		"plus_di14",
		"minus_di14",
		"stc",
		"stc_delta",
		"vwap_distance_pct",
		"dynamic_swing_vwap_distance_pct",
		"dynamic_swing_vwap_anchor_age",
		"squeeze_momentum",
		"squeeze_momentum_delta",
		"price_ema7_distance_pct",
		"price_ema25_distance_pct",
		"price_ema99_distance_pct",
		"ema25_slope5_pct",
	} {
		series, ok := window.Numeric(key)
		if !ok {
			continue
		}
		values[key] = formatFloat(series.Latest)
		values[key+"_previous"] = formatFloat(series.Previous)
	}
	for _, key := range []string{
		"supertrend_direction",
		"supertrend_zone_area",
		"stc_direction",
		"stc_zone",
		"stc_cross",
		"ema_alignment",
		"trend_direction",
		"volatility_window_state",
	} {
		value := latestSignal(window, key)
		if value != "" {
			values[key] = value
		}
	}
	return infoDiagnostic("entry_feature_snapshot", side, "entry features captured for offline analysis", values)
}

func entryQualityScore(regime string, fiveMinuteState string, momentumConfirmations string, volatilityState string) (float64, map[string]string) {
	const baseScore = 0.50
	regimeAdjustment := 0.0
	switch normalize(regime) {
	case "macro_blocked", "countertrend":
		regimeAdjustment = 0.10
	case "trend":
		regimeAdjustment = -0.15
	}
	fiveMinuteAdjustment := 0.0
	switch normalize(fiveMinuteState) {
	case "blocking":
		fiveMinuteAdjustment = 0.10
	case "aligned":
		fiveMinuteAdjustment = -0.10
	}
	momentumAdjustment := 0.0
	switch momentumConfirmations {
	case "1":
		momentumAdjustment = 0.05
	case "4":
		momentumAdjustment = -0.10
	}
	volatilityAdjustment := 0.0
	if normalize(volatilityState) == "contracting" {
		volatilityAdjustment = -0.05
	}
	score := baseScore + regimeAdjustment + fiveMinuteAdjustment + momentumAdjustment + volatilityAdjustment
	return score, map[string]string{
		"base":                   formatFloat(baseScore),
		"regime":                 regime,
		"regime_adjustment":      formatFloat(regimeAdjustment),
		"five_minute_state":      fiveMinuteState,
		"five_minute_adjustment": formatFloat(fiveMinuteAdjustment),
		"momentum_confirmations": momentumConfirmations,
		"momentum_adjustment":    formatFloat(momentumAdjustment),
		"volatility_state":       volatilityState,
		"volatility_adjustment":  formatFloat(volatilityAdjustment),
		"quality_score":          formatFloat(score),
	}
}

func withThresholdCheck(decision entryDecision, threshold float64) entryDecision {
	passed := !decision.blocked && decision.score >= threshold
	decision.checks = append(decision.checks, diagnosticCheck("entry_threshold", decision.side, passed, 0, checkReason(!passed, "entry threshold not met", "entry threshold met"), map[string]string{
		"score": strconv.FormatFloat(decision.score, 'f', -1, 64), "threshold": strconv.FormatFloat(threshold, 'f', -1, 64),
	}))
	return decision
}

func diagnosticCheck(name string, side strategy.SignalSide, passed bool, score float64, reason string, values map[string]string) strategy.DiagnosticCheck {
	status := strategy.DiagnosticStatusBlocked
	if passed {
		status = strategy.DiagnosticStatusPass
	}
	return strategy.DiagnosticCheck{Name: name, Side: side, Status: status, Score: score, Reason: reason, Values: values}
}

func entryConfirmationCheck(name string, side strategy.SignalSide, passed bool, blocking bool, score float64, reason string, values map[string]string) strategy.DiagnosticCheck {
	if passed || blocking {
		return diagnosticCheck(name, side, passed, score, reason, values)
	}
	return infoDiagnostic(name, side, reason+"; non-blocking confirmation", values)
}

func stcObservationCheck(window strategy.IndicatorWindowView, side strategy.SignalSide) strategy.DiagnosticCheck {
	series, ok := window.Numeric("stc")
	if !ok {
		return strategy.DiagnosticCheck{Name: "stc", Side: side, Status: strategy.DiagnosticStatusMissing, Reason: "stc unavailable"}
	}
	values := map[string]string{
		"value":     formatFloat(series.Latest),
		"previous":  formatFloat(series.Previous),
		"delta":     formatFloat(series.Latest - series.Previous),
		"direction": latestSignal(window, "stc_direction"),
		"zone":      latestSignal(window, "stc_zone"),
		"cross":     latestSignal(window, "stc_cross"),
	}
	return infoDiagnostic("stc", side, "stc observed without decision impact", values)
}

func stcEntryCheck(window strategy.IndicatorWindowView, side strategy.SignalSide) (strategy.DiagnosticCheck, bool) {
	check := stcObservationCheck(window, side)
	if check.Values == nil {
		return check, false
	}
	cross := check.Values["cross"]
	vetoed := (side == strategy.SignalSideBuy && cross == "up_25") ||
		(side == strategy.SignalSideSell && cross == "down_25")
	check.Values["entry_veto"] = strconv.FormatBool(vetoed)
	if vetoed {
		check.Status = strategy.DiagnosticStatusBlocked
		check.Reason = "stc 25-line entry veto"
	} else {
		check.Reason = "stc entry state accepted"
	}
	return check, vetoed
}

func checkReason(blocked bool, blockedReason string, passReason string) string {
	if blocked {
		return blockedReason
	}
	return passReason
}

func selectEntry(longDecision entryDecision, shortDecision entryDecision, threshold float64) entryDecision {
	hold := entryDecision{side: strategy.SignalSideHold}
	longOK := !longDecision.blocked && longDecision.score >= threshold
	shortOK := !shortDecision.blocked && shortDecision.score >= threshold
	switch {
	case longOK && shortOK:
		if longDecision.score >= shortDecision.score {
			return longDecision
		}
		return shortDecision
	case longOK:
		return longDecision
	case shortOK:
		return shortDecision
	default:
		return hold
	}
}

func (s *Strategy) signal(
	snapshot strategy.Snapshot,
	side strategy.SignalSide,
	score float64,
	reason string,
	exitRules []strategy.ExitRule,
) strategy.Result {
	return s.signalWithChecks(snapshot, side, score, reason, exitRules, nil)
}

func (s *Strategy) signalWithChecks(snapshot strategy.Snapshot, side strategy.SignalSide, score float64, reason string, exitRules []strategy.ExitRule, checks []strategy.DiagnosticCheck) strategy.Result {
	return strategy.Result{
		StrategyName: Name,
		Signal: strategy.Signal{
			Exchange:   snapshot.Target.Exchange,
			Market:     snapshot.Target.Market,
			Symbol:     snapshot.Target.Symbol,
			Interval:   snapshot.Target.Interval,
			Strategy:   Name,
			Side:       side,
			Score:      score,
			Confidence: score,
			Reason:     reason,
			OpenTime:   snapshot.Window.OpenTime,
			UpdatedAt:  snapshot.UpdatedAt,
		},
		Analysis: strategy.Analysis{
			Summary: reason,
			Checks:  checks,
		},
		ExitRules: exitRules,
	}
}

func (s *Strategy) hold(snapshot strategy.Snapshot, reason string) strategy.Result {
	return s.holdWithChecks(snapshot, reason, nil)
}

func (s *Strategy) holdWithChecks(snapshot strategy.Snapshot, reason string, checks []strategy.DiagnosticCheck) strategy.Result {
	if strings.TrimSpace(reason) == "" {
		reason = "no actionable signal"
	}
	return s.signalWithChecks(snapshot, strategy.SignalSideHold, 0, reason, nil, checks)
}

func dataQualityOK(window strategy.IndicatorWindowView) bool {
	quality := latestSignal(window, "data_quality")
	return quality == "" || quality == "ok"
}

func entryTriggered(window strategy.IndicatorWindowView, side strategy.SignalSide) bool {
	return len(supertrendEntryTriggerSources(standardEntryTriggerSources(window, side))) > 0
}

func supertrendEntryTriggerSources(sources []string) []string {
	result := make([]string, 0, 2)
	for _, source := range sources {
		if source == "wick_reclaim" || source == "supertrend_flip" {
			result = append(result, source)
		}
	}
	return result
}

func standardEntryTriggerSources(window strategy.IndicatorWindowView, side strategy.SignalSide) []string {
	sources := make([]string, 0, 2)
	if previousWickReclaimed(window, side) {
		sources = append(sources, "wick_reclaim")
	}
	if freshSignalValue(window, "supertrend_direction", directionForSide(side)) {
		sources = append(sources, "supertrend_flip")
	}
	return sources
}

func entryTriggerValues(_ strategy.IndicatorWindowView, _ strategy.SignalSide, standardSources []string) map[string]string {
	return map[string]string{
		"standard_trigger_sources": strings.Join(standardSources, ","),
		"standard_trigger_count":   strconv.Itoa(len(standardSources)),
		"wick_reclaim":             strconv.FormatBool(containsString(standardSources, "wick_reclaim")),
		"supertrend_flip":          strconv.FormatBool(containsString(standardSources, "supertrend_flip")),
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func previousWickReclaimed(window strategy.IndicatorWindowView, side strategy.SignalSide) bool {
	closeSeries, closeOK := window.Numeric("close")
	supertrendSeries, supertrendOK := window.Numeric("supertrend")
	if !closeOK || !supertrendOK || closeSeries.Previous <= 0 || supertrendSeries.Previous <= 0 {
		return false
	}
	switch side {
	case strategy.SignalSideBuy:
		lowSeries, ok := window.Numeric("low")
		return ok && lowSeries.Previous > 0 &&
			lowSeries.Previous < supertrendSeries.Previous &&
			closeSeries.Previous > supertrendSeries.Previous &&
			latestSignal(window, "supertrend_direction") == "up"
	case strategy.SignalSideSell:
		highSeries, ok := window.Numeric("high")
		return ok && highSeries.Previous > 0 &&
			highSeries.Previous > supertrendSeries.Previous &&
			closeSeries.Previous < supertrendSeries.Previous &&
			latestSignal(window, "supertrend_direction") == "down"
	default:
		return false
	}
}

func fakeRiskLevel(window strategy.IndicatorWindowView, side strategy.SignalSide) string {
	key := "pump_window_fake_risk"
	if side == strategy.SignalSideSell {
		key = "dump_window_fake_risk"
	}
	return latestSignal(window, key)
}

func localTrendAligned(window strategy.IndicatorWindowView, side strategy.SignalSide) bool {
	expectedBias := biasForSide(side)
	expectedDirection := directionForSide(side)
	return latestSignal(window, "trend_window_bias") == expectedBias ||
		latestSignal(window, "supertrend_direction") == expectedDirection ||
		latestSignal(window, "alphatrend_direction") == expectedDirection
}

func trendConfirmationForEntry(window strategy.IndicatorWindowView, side strategy.SignalSide) (bool, float64, []string) {
	direction := directionForSide(side)
	oppositeDirection := oppositeDirectionForSide(side)
	oppositeBias := oppositeBias(side)
	if latestSignal(window, "trend_window_bias") == oppositeBias ||
		latestSignal(window, "supertrend_direction") == oppositeDirection ||
		latestSignal(window, "alphatrend_direction") == oppositeDirection {
		return false, 0, []string{"trend opposed"}
	}
	progress := latestSignal(window, "trend_price_progress")
	if progress != "" && progress != "advancing" {
		return true, 0, []string{"trend progress not advancing; score withheld"}
	}
	if truthySignal(window, "trend_window_reversal_risk") {
		return false, 0, []string{"trend reversal risk blocked"}
	}
	aligned := latestSignal(window, "trend_window_bias") == direction ||
		latestSignal(window, "supertrend_direction") == direction ||
		latestSignal(window, "alphatrend_direction") == direction ||
		latestSignal(window, "trend_window_bias") == biasForSide(side)
	if !aligned {
		return true, 0, []string{"trend neutral"}
	}
	if valid := latestSignal(window, "trend_valid"); valid != "" && !truthy(valid) {
		return true, 0, []string{"trend aligned but composite validity unconfirmed"}
	}
	quality := latestSignal(window, "trend_quality")
	switch quality {
	case "weak":
		return true, 0, []string{"trend aligned but quality weak"}
	case "flat", "choppy":
		return false, 0, []string{"trend quality blocked"}
	default:
		return true, 0.16, []string{"trend aligned"}
	}
}

func maConfirmationForEntry(window strategy.IndicatorWindowView, side strategy.SignalSide) (bool, float64, []string) {
	expected := biasForSide(side)
	opposite := oppositeBias(side)
	if supportsBias(latestSignal(window, "ma_ribbon_state"), opposite) ||
		latestSignal(window, "ma_window_bias") == opposite ||
		latestSignal(window, "ema_alignment") == opposite {
		return false, 0, []string{"ma opposed"}
	}
	tangled := truthySignal(window, "ma_window_tangled")
	for _, key := range []string{"ma_ribbon_state", "ma_ribbon_phase"} {
		if signalIs(latestSignal(window, key), "tangled", "flat", "range", "choppy", "compressing") {
			tangled = true
		}
	}
	if tangled {
		return true, 0, []string{"ma window tangled; score withheld"}
	}
	if supportsBias(latestSignal(window, "ma_ribbon_state"), expected) ||
		latestSignal(window, "ma_window_bias") == expected ||
		latestSignal(window, "ema_alignment") == expected {
		return true, 0.14, []string{"ma aligned"}
	}
	return true, 0, []string{"ma neutral"}
}

func trendConfirmation(window strategy.IndicatorWindowView, side strategy.SignalSide) (bool, float64, []string) {
	reasons := []string{}
	if valid := latestSignal(window, "trend_valid"); valid != "" && !truthy(valid) {
		return false, 0, []string{"trend invalid"}
	}
	direction := directionForSide(side)
	oppositeDirection := oppositeDirectionForSide(side)
	oppositeBias := oppositeBias(side)
	if latestSignal(window, "trend_window_bias") == oppositeBias ||
		latestSignal(window, "supertrend_direction") == oppositeDirection ||
		latestSignal(window, "alphatrend_direction") == oppositeDirection {
		return false, 0, []string{"trend opposed"}
	}
	if latestSignal(window, "trend_window_bias") == direction ||
		latestSignal(window, "supertrend_direction") == direction ||
		latestSignal(window, "alphatrend_direction") == direction ||
		latestSignal(window, "trend_window_bias") == biasForSide(side) {
		reasons = append(reasons, "trend aligned")
	} else {
		return true, 0, []string{"trend neutral"}
	}
	progress := latestSignal(window, "trend_price_progress")
	if progress != "" && progress != "advancing" {
		return false, 0, []string{"trend progress blocked"}
	}
	quality := latestSignal(window, "trend_quality")
	if quality == "flat" || quality == "weak" {
		return false, 0, []string{"trend quality blocked"}
	}
	return true, 0.16, reasons
}

func maConfirmation(window strategy.IndicatorWindowView, side strategy.SignalSide) (bool, float64, []string) {
	if truthySignal(window, "ma_window_tangled") {
		return false, 0, []string{"ma window tangled"}
	}
	for _, key := range []string{"ma_ribbon_state", "ma_ribbon_phase"} {
		value := latestSignal(window, key)
		if signalIs(value, "tangled", "flat", "range", "choppy", "compressing") {
			return false, 0, []string{"ma ribbon blocked"}
		}
	}
	expected := biasForSide(side)
	opposite := oppositeBias(side)
	if supportsBias(latestSignal(window, "ma_ribbon_state"), opposite) ||
		latestSignal(window, "ma_window_bias") == opposite ||
		latestSignal(window, "ema_alignment") == opposite {
		return false, 0, []string{"ma opposed"}
	}
	if supportsBias(latestSignal(window, "ma_ribbon_state"), expected) ||
		latestSignal(window, "ma_window_bias") == expected ||
		latestSignal(window, "ema_alignment") == expected {
		return true, 0.14, []string{"ma aligned"}
	}
	return true, 0, []string{"ma neutral"}
}

func macdConfirmation(window strategy.IndicatorWindowView, side strategy.SignalSide) (bool, float64, []string) {
	expected := biasForSide(side)
	opposite := oppositeBias(side)
	if latestSignal(window, "macd_window_bias") == opposite || supportsBias(latestSignal(window, "macd_momentum"), opposite) {
		return false, 0, []string{"macd blocked"}
	}
	divergence := latestSignal(window, "macd_divergence")
	if side == strategy.SignalSideBuy && divergence == "bearish" {
		return false, 0, []string{"macd bearish divergence"}
	}
	if side == strategy.SignalSideSell && divergence == "bullish" {
		return false, 0, []string{"macd bullish divergence"}
	}
	if latestSignal(window, "macd_window_bias") == expected || supportsBias(latestSignal(window, "macd_momentum"), expected) {
		return true, 0.14, []string{"macd aligned"}
	}
	return true, 0, []string{"macd neutral"}
}

func volumeConfirmation(window strategy.IndicatorWindowView, side strategy.SignalSide) (bool, float64, []string) {
	confirmation := latestSignal(window, "price_volume_confirmation")
	if side == strategy.SignalSideBuy && confirmation == "divergence_bear" {
		return false, 0, []string{"price-volume bearish divergence"}
	}
	if side == strategy.SignalSideSell && confirmation == "divergence_bull" {
		return false, 0, []string{"price-volume bullish divergence"}
	}
	score := 0.0
	reasons := []string{}
	expected := "confirm_up"
	if side == strategy.SignalSideSell {
		expected = "confirm_down"
	}
	if confirmation == expected {
		score += 0.05
		reasons = append(reasons, "price-volume confirmed")
	}
	switch latestSignal(window, "volume_window_state") {
	case "spike", "expanding", "expansion", "breakout", "above_average", "climax":
		score += 0.03
		reasons = append(reasons, "volume expanded")
	}
	return true, score, reasons
}

func momentumEnergy(window strategy.IndicatorWindowView, side strategy.SignalSide) (bool, float64, []string, map[string]string) {
	expectedBias := biasForSide(side)
	maSlope := latestSignal(window, "ma_window_slope_level")
	maSlopeAligned := signalIs(maSlope, "rising", "steep_up")
	macdAcceleration := "rising"
	expectedMomentum := "expanding_bull"
	expectedVolume := "confirm_up"
	if side == strategy.SignalSideSell {
		maSlopeAligned = signalIs(maSlope, "falling", "steep_down")
		macdAcceleration = "falling"
		expectedMomentum = "expanding_bear"
		expectedVolume = "confirm_down"
	}

	maEnergy := !truthySignal(window, "ma_window_tangled") &&
		(latestSignal(window, "ma_window_bias") == expectedBias || supportsBias(latestSignal(window, "ma_ribbon_state"), expectedBias)) &&
		signalIs(latestSignal(window, "ma_window_spread_state"), "rising", "expanding") &&
		maSlopeAligned
	macdEnergy := (latestSignal(window, "macd_window_bias") == expectedBias && truthySignal(window, "macd_window_confirmed")) ||
		signalIs(latestSignal(window, "macd_momentum"), expectedMomentum) ||
		(latestSignal(window, "macd_window_bias") == expectedBias && signalIs(latestSignal(window, "macd_window_acceleration"), macdAcceleration))
	priceVolumeEnergy := signalIs(latestSignal(window, "price_volume_confirmation"), expectedVolume)
	volumeExpansion := signalIs(latestSignal(window, "volume_window_state"),
		"spike", "expanding", "expansion", "breakout", "above_average", "climax")

	values := map[string]string{
		"ma":                     strconv.FormatBool(maEnergy),
		"macd":                   strconv.FormatBool(macdEnergy),
		"price_volume":           strconv.FormatBool(priceVolumeEnergy),
		"volume_expansion":       strconv.FormatBool(volumeExpansion),
		"required_confirmations": "2",
	}
	confirmations := 0
	reasons := []string{}
	for _, evidence := range []struct {
		confirmed bool
		reason    string
	}{
		{maEnergy, "ma expanding"},
		{macdEnergy, "macd accelerating"},
		{priceVolumeEnergy, "price-volume directional"},
		{volumeExpansion, "volume expanding"},
	} {
		if evidence.confirmed {
			confirmations++
			reasons = append(reasons, evidence.reason)
		}
	}
	values["confirmations"] = strconv.Itoa(confirmations)
	passed := confirmations >= 2
	if !passed {
		reasons = append(reasons, "momentum energy insufficient")
	}
	return passed, 0.02 * float64(confirmations), reasons, values
}

func classifyTimeframes(snapshot strategy.Snapshot, side strategy.SignalSide) (int, int) {
	aligned := 0
	blocked := 0
	for interval, timeframe := range snapshot.Timeframes {
		if interval == snapshot.Target.Interval {
			continue
		}
		switch classifyTimeframe(timeframe.Window, side) {
		case "aligned":
			aligned++
		case "blocking":
			blocked++
		}
	}
	return aligned, blocked
}

func higherTimeframeAuthorized(snapshot strategy.Snapshot, side strategy.SignalSide) (bool, map[string]string) {
	ten, tenOK := snapshot.Timeframes["10m"]
	fifteen, fifteenOK := snapshot.Timeframes["15m"]
	thirty, thirtyOK := snapshot.Timeframes["30m"]
	values := map[string]string{
		"10m":   "missing",
		"15m":   "missing",
		"30m":   "missing",
		"state": "missing",
	}
	if !tenOK || !fifteenOK || !thirtyOK {
		return false, values
	}
	tenState := classifyTimeframe(ten.Window, side)
	fifteenState := classifyTimeframe(fifteen.Window, side)
	thirtyState := classifyTimeframe(thirty.Window, side)
	tenStable := latestSignal(ten.Window, "supertrend_direction") == directionForSide(side) &&
		latestSignalStableCount(ten.Window, "supertrend_direction") > 1
	values["10m"] = tenState
	values["15m"] = fifteenState
	values["30m"] = thirtyState
	values["10m_stable"] = strconv.FormatBool(tenStable)

	switch {
	case thirtyState == "blocking":
		values["state"] = "macro_blocked"
	case fifteenState == "blocking" && tenState == "aligned":
		values["state"] = "transition"
	case fifteenState == "blocking":
		values["state"] = "countertrend"
	case fifteenState == "aligned" && tenState == "blocking":
		values["state"] = "pullback"
	case fifteenState == "aligned" && tenState == "aligned" && !tenStable:
		values["state"] = "stabilizing"
	case fifteenState == "aligned" && tenState == "aligned":
		values["state"] = "trend"
	default:
		values["state"] = "neutral"
	}
	return values["state"] != "macro_blocked" && values["state"] != "countertrend", values
}

func pullbackResolved(snapshot strategy.Snapshot, side strategy.SignalSide) (bool, map[string]string) {
	five, ok := snapshot.Timeframes["5m"]
	values := map[string]string{"5m": "missing"}
	if !ok {
		return false, values
	}
	state := classifyTimeframe(five.Window, side)
	values["5m"] = state
	return state == "aligned", values
}

func shortTimeframesBlocked(snapshot strategy.Snapshot, side strategy.SignalSide) bool {
	five, fiveOK := snapshot.Timeframes["5m"]
	ten, tenOK := snapshot.Timeframes["10m"]
	return fiveOK && tenOK &&
		classifyTimeframe(five.Window, side) == "blocking" &&
		classifyTimeframe(ten.Window, side) == "blocking"
}

func classifyTimeframe(window strategy.IndicatorWindowView, side strategy.SignalSide) string {
	if window.Empty() {
		return "missing"
	}
	if truthySignal(window, "ma_window_tangled") {
		return "neutral"
	}
	expected := biasForSide(side)
	opposite := oppositeBias(side)
	direction := directionForSide(side)
	oppositeDirection := oppositeDirectionForSide(side)
	aligned := 0
	blocking := 0
	for _, value := range []string{
		latestSignal(window, "trend_window_bias"),
		latestSignal(window, "ma_window_bias"),
		latestSignal(window, "macd_window_bias"),
	} {
		if value == expected {
			aligned++
		}
		if value == opposite {
			blocking++
		}
	}
	if latestSignal(window, "supertrend_direction") == direction {
		aligned++
	}
	if latestSignal(window, "supertrend_direction") == oppositeDirection {
		blocking++
	}
	if side == strategy.SignalSideBuy && truthySignal(window, "dump_window_signal") {
		blocking++
	}
	if side == strategy.SignalSideSell && truthySignal(window, "pump_window_signal") {
		blocking++
	}
	if blocking > aligned && blocking > 0 {
		return "blocking"
	}
	if aligned > 0 {
		return "aligned"
	}
	return "neutral"
}

func higherTimeframeReversed(snapshot strategy.Snapshot, side strategy.SignalSide) (bool, map[string]string) {
	ten, tenOK := snapshot.Timeframes["10m"]
	fifteen, fifteenOK := snapshot.Timeframes["15m"]
	values := map[string]string{
		"10m": "missing",
		"15m": "missing",
	}
	if !tenOK || !fifteenOK {
		return false, values
	}
	tenState := classifyTimeframe(ten.Window, side)
	fifteenState := classifyTimeframe(fifteen.Window, side)
	values["10m"] = tenState
	values["15m"] = fifteenState
	return tenState == "aligned" && fifteenState == "aligned", values
}

func adaptiveDirectionInvalidated(
	snapshot strategy.Snapshot,
	currentPosition *strategy.Position,
	holdingSide strategy.SignalSide,
) (strategy.DiagnosticCheck, bool) {
	window := snapshot.Window
	oppositeBiasValue := oppositeBias(holdingSide)
	oppositeDirection := oppositeDirectionForSide(holdingSide)
	trendOpposed := latestSignal(window, "supertrend_direction") == oppositeDirection ||
		latestSignal(window, "trend_window_bias") == oppositeBiasValue ||
		latestSignal(window, "alphatrend_direction") == oppositeDirection ||
		truthySignal(window, "trend_window_reversal_risk")
	maOpposed := supportsBias(latestSignal(window, "ma_ribbon_state"), oppositeBiasValue) ||
		latestSignal(window, "ma_window_bias") == oppositeBiasValue ||
		latestSignal(window, "ema_alignment") == oppositeBiasValue
	macdDivergence := "bearish"
	oppositeVolume := "confirm_down"
	volumeDivergence := "divergence_bear"
	if holdingSide == strategy.SignalSideSell {
		macdDivergence = "bullish"
		oppositeVolume = "confirm_up"
		volumeDivergence = "divergence_bull"
	}
	macdOpposed := latestSignal(window, "macd_window_bias") == oppositeBiasValue ||
		supportsBias(latestSignal(window, "macd_momentum"), oppositeBiasValue) ||
		latestSignal(window, "macd_divergence") == macdDivergence
	priceVolume := latestSignal(window, "price_volume_confirmation")
	volumeOpposed := priceVolume == oppositeVolume || priceVolume == volumeDivergence

	confirmations := 0
	for _, opposed := range []bool{trendOpposed, maOpposed, macdOpposed, volumeOpposed} {
		if opposed {
			confirmations++
		}
	}
	fiveState := "missing"
	if five, ok := snapshot.Timeframes["5m"]; ok {
		fiveState = classifyTimeframe(five.Window, holdingSide)
	}
	moveBps, moveOK := currentPositionMoveBps(snapshot, currentPosition)
	invalidated := fiveState == "blocking" && confirmations >= 1
	if moveOK && moveBps <= 0 && confirmations >= 2 {
		invalidated = true
	}
	if moveOK && moveBps > 0 && confirmations >= 3 {
		invalidated = true
	}
	state := "healthy"
	if invalidated {
		state = "invalidated"
	} else if confirmations > 0 || fiveState == "blocking" {
		state = "watch"
	}
	values := map[string]string{
		"state":                  state,
		"current_move_bps":       formatFloat(moveBps),
		"current_move_available": strconv.FormatBool(moveOK),
		"confirmations":          strconv.Itoa(confirmations),
		"trend_opposed":          strconv.FormatBool(trendOpposed),
		"ma_opposed":             strconv.FormatBool(maOpposed),
		"macd_opposed":           strconv.FormatBool(macdOpposed),
		"volume_opposed":         strconv.FormatBool(volumeOpposed),
		"5m":                     fiveState,
	}
	reason := "intraday direction remains valid"
	if invalidated {
		reason = "intraday direction invalidated"
	} else if state == "watch" {
		reason = "intraday direction deterioration detected"
	}
	return infoDiagnostic("direction_invalidation", holdingSide, reason, values), invalidated
}

func currentPositionMoveBps(snapshot strategy.Snapshot, currentPosition *strategy.Position) (float64, bool) {
	if currentPosition == nil {
		return 0, false
	}
	entryPrice, err := strconv.ParseFloat(currentPosition.EntryPrice, 64)
	if err != nil || entryPrice <= 0 {
		return 0, false
	}
	currentPrice, ok := numericValue(snapshot, "close")
	if !ok {
		return 0, false
	}
	switch currentPosition.Side {
	case strategy.PositionSideLong:
		return (currentPrice - entryPrice) / entryPrice * 10000, true
	case strategy.PositionSideShort:
		return (entryPrice - currentPrice) / entryPrice * 10000, true
	default:
		return 0, false
	}
}

func (s *Strategy) profitProtectionCheck(
	snapshot strategy.Snapshot,
	currentPosition *strategy.Position,
	holdingSide strategy.SignalSide,
) (strategy.DiagnosticCheck, string) {
	mfeBps, ok := favorableExcursionBps(currentPosition)
	activationBps := s.config.ProfitDecayActivationBps
	if s.config.ExitMode == ExitModeAdaptive {
		if entryPrice, err := strconv.ParseFloat(currentPosition.EntryPrice, 64); err == nil && entryPrice > 0 {
			activationBps = s.adaptiveExitParameters(snapshot, entryPrice).decayActivationBps
		}
	}
	values := map[string]string{
		"state":                  "inactive",
		"mfe_bps":                formatFloat(mfeBps),
		"activation_bps":         formatFloat(activationBps),
		"exit_mode":              s.config.ExitMode,
		"decay_confirmations":    "0",
		"momentum_confirmations": "0",
		"5m":                     "missing",
		"10m":                    "missing",
		"15m":                    "missing",
	}
	profitProtectionEnabled := s.config.ExitMode == ExitModeTrailing || s.config.ExitMode == ExitModeAdaptive
	if !profitProtectionEnabled || !ok || activationBps <= 0 || mfeBps < activationBps {
		return infoDiagnostic("profit_protection", holdingSide, "profit protection not activated", values), "inactive"
	}

	for _, interval := range []string{"5m", "10m", "15m"} {
		if timeframe, exists := snapshot.Timeframes[interval]; exists {
			values[interval] = classifyTimeframe(timeframe.Window, holdingSide)
		}
	}
	_, _, _, energyValues := momentumEnergy(snapshot.Window, holdingSide)
	momentumConfirmations, _ := strconv.Atoi(energyValues["confirmations"])
	values["momentum_confirmations"] = strconv.Itoa(momentumConfirmations)
	decayConfirmations := localDecayConfirmations(snapshot.Window, holdingSide)
	values["decay_confirmations"] = strconv.Itoa(decayConfirmations)

	state := "transition"
	if values["5m"] != "aligned" && decayConfirmations >= 2 {
		state = "weak_exit"
	} else if values["5m"] == "aligned" && values["10m"] == "aligned" && values["15m"] == "aligned" &&
		momentumConfirmations >= 3 && latestSignal(snapshot.Window, "trend_price_progress") == "advancing" &&
		!truthySignal(snapshot.Window, "trend_window_reversal_risk") {
		state = "strong_runner"
	}
	values["state"] = state
	reason := "protected profit remains in transition"
	if state == "strong_runner" {
		reason = "protected profit trend remains strong"
	} else if state == "weak_exit" {
		reason = "protected profit momentum decayed"
	}
	return infoDiagnostic("profit_protection", holdingSide, reason, values), state
}

func favorableExcursionBps(currentPosition *strategy.Position) (float64, bool) {
	if currentPosition == nil {
		return 0, false
	}
	entryPrice, err := strconv.ParseFloat(currentPosition.EntryPrice, 64)
	if err != nil || entryPrice <= 0 {
		return 0, false
	}
	switch currentPosition.Side {
	case strategy.PositionSideLong:
		highestPrice, parseErr := strconv.ParseFloat(currentPosition.HighestPrice, 64)
		if parseErr != nil || highestPrice < entryPrice {
			return 0, false
		}
		return (highestPrice - entryPrice) / entryPrice * 10000, true
	case strategy.PositionSideShort:
		lowestPrice, parseErr := strconv.ParseFloat(currentPosition.LowestPrice, 64)
		if parseErr != nil || lowestPrice <= 0 || lowestPrice > entryPrice {
			return 0, false
		}
		return (entryPrice - lowestPrice) / entryPrice * 10000, true
	default:
		return 0, false
	}
}

func localDecayConfirmations(window strategy.IndicatorWindowView, side strategy.SignalSide) int {
	confirmations := 0
	progress := latestSignal(window, "trend_price_progress")
	if truthySignal(window, "trend_window_reversal_risk") || (progress != "" && progress != "advancing") {
		confirmations++
	}
	spreadState := latestSignal(window, "ma_window_spread_state")
	if truthySignal(window, "ma_window_tangled") || (spreadState != "" && !signalIs(spreadState, "rising", "expanding")) {
		confirmations++
	}
	expectedAcceleration := "rising"
	oppositeDivergence := "bearish"
	expectedVolume := "confirm_up"
	if side == strategy.SignalSideSell {
		expectedAcceleration = "falling"
		oppositeDivergence = "bullish"
		expectedVolume = "confirm_down"
	}
	acceleration := latestSignal(window, "macd_window_acceleration")
	if latestSignal(window, "macd_divergence") == oppositeDivergence || (acceleration != "" && acceleration != expectedAcceleration) {
		confirmations++
	}
	priceVolume := latestSignal(window, "price_volume_confirmation")
	if priceVolume != "" && priceVolume != expectedVolume {
		confirmations++
	}
	return confirmations
}

func infoDiagnostic(name string, side strategy.SignalSide, reason string, values map[string]string) strategy.DiagnosticCheck {
	return strategy.DiagnosticCheck{Name: name, Side: side, Status: strategy.DiagnosticStatusInfo, Reason: reason, Values: values}
}

func reverseConfirmed(snapshot strategy.Snapshot, side strategy.SignalSide) bool {
	window := snapshot.Window
	if !entryTriggered(window, side) {
		return false
	}
	trendOK, _, _ := trendConfirmation(window, side)
	maOK, _, _ := maConfirmation(window, side)
	macdOK, _, _ := macdConfirmation(window, side)
	if trendOK && maOK && macdOK {
		return true
	}
	if shortTimeframesBlocked(snapshot, oppositeSide(side)) {
		return true
	}
	direction := latestSignal(window, "supertrend_direction")
	return direction == directionForSide(side) && latestSignalStableCount(window, "supertrend_direction") > 1
}

func (s *Strategy) exitGeometryGuardEnabled() bool {
	return s.config.MinTakeProfitBps > 0 || s.config.MinRewardRiskRatio > 0
}

func (s *Strategy) exitGeometryCheck(snapshot strategy.Snapshot, side strategy.SignalSide, rules []strategy.ExitRule) strategy.DiagnosticCheck {
	entry, entryOK := numericValue(snapshot, "close")
	takeProfit, takeProfitOK := exitRulePrice(rules, strategy.ExitReasonTakeProfit)
	stopLoss, stopLossOK := exitRulePrice(rules, strategy.ExitReasonStopLoss)
	values := map[string]string{
		"entry":                 formatFloat(entry),
		"take_profit":           formatFloat(takeProfit),
		"stop_loss":             formatFloat(stopLoss),
		"min_take_profit_bps":   formatFloat(s.config.MinTakeProfitBps),
		"min_reward_risk_ratio": formatFloat(s.config.MinRewardRiskRatio),
	}
	if !entryOK || !takeProfitOK || !stopLossOK {
		return diagnosticCheck("exit_geometry", side, false, 0, "exit geometry inputs missing", values)
	}

	var rewardBps, riskBps float64
	switch side {
	case strategy.SignalSideBuy:
		rewardBps = (takeProfit - entry) / entry * 10000
		riskBps = (entry - stopLoss) / entry * 10000
	case strategy.SignalSideSell:
		rewardBps = (entry - takeProfit) / entry * 10000
		riskBps = (stopLoss - entry) / entry * 10000
	default:
		return diagnosticCheck("exit_geometry", side, false, 0, "exit side is not actionable", values)
	}
	values["reward_bps"] = formatFloat(rewardBps)
	values["risk_bps"] = formatFloat(riskBps)
	if rewardBps <= 0 || riskBps <= 0 {
		return diagnosticCheck("exit_geometry", side, false, 0, "take profit or stop loss is on the wrong side of entry", values)
	}

	rewardRiskRatio := rewardBps / riskBps
	values["reward_risk_ratio"] = formatFloat(rewardRiskRatio)
	if s.config.MinTakeProfitBps > 0 && rewardBps < s.config.MinTakeProfitBps {
		return diagnosticCheck("exit_geometry", side, false, 0, "take profit distance below minimum", values)
	}
	if s.config.MinRewardRiskRatio > 0 && rewardRiskRatio < s.config.MinRewardRiskRatio {
		return diagnosticCheck("exit_geometry", side, false, 0, "reward-risk ratio below minimum", values)
	}
	return diagnosticCheck("exit_geometry", side, true, 0, "exit geometry accepted", values)
}

func (s *Strategy) exitRules(snapshot strategy.Snapshot, side strategy.SignalSide) []strategy.ExitRule {
	if s.config.ExitMode == ExitModeTrailing {
		return buildTrailingExitRules(
			numericString(snapshot, "close"),
			s.hardRiskStopLoss(snapshot, side),
			s.config.TrailingStopPct,
			s.config.ProfitGuardActivationBps,
			s.config.ProfitGuardFloorBps,
		)
	}
	if s.config.ExitMode == ExitModeAdaptive {
		referencePrice, ok := numericValue(snapshot, "close")
		if !ok {
			return nil
		}
		params := s.adaptiveExitParameters(snapshot, referencePrice)
		return buildAdaptiveExitRules(
			formatFloat(referencePrice),
			s.hardRiskStopLoss(snapshot, side),
			params,
		)
	}
	if side == strategy.SignalSideBuy {
		return buildExitRules(s.costFlooredTakeProfit(snapshot, side, "resistance_1"), s.boundedStopLoss(snapshot, side, "supertrend", "support_1"))
	}
	return buildExitRules(s.costFlooredTakeProfit(snapshot, side, "support_1"), s.boundedStopLoss(snapshot, side, "supertrend", "resistance_1"))
}

func hasExitRule(rules []strategy.ExitRule, ruleType strategy.ExitReasonType) bool {
	for _, rule := range rules {
		if rule.Type == ruleType {
			return true
		}
	}
	return false
}

func exitRulePrice(rules []strategy.ExitRule, ruleType strategy.ExitReasonType) (float64, bool) {
	for _, rule := range rules {
		if rule.Type != ruleType {
			continue
		}
		value, err := strconv.ParseFloat(rule.TriggerPrice, 64)
		return value, err == nil && value > 0
	}
	return 0, false
}

func buildExitRules(takeProfit string, stopLoss string) []strategy.ExitRule {
	rules := []strategy.ExitRule{}
	if takeProfit != "" {
		rules = append(rules, strategy.ExitRule{
			Type:         strategy.ExitReasonTakeProfit,
			Reason:       "strategy take profit",
			TriggerPrice: takeProfit,
			SizePct:      1,
		})
	}
	if stopLoss != "" {
		rules = append(rules, strategy.ExitRule{
			Type:         strategy.ExitReasonStopLoss,
			Reason:       "strategy stop loss",
			TriggerPrice: stopLoss,
			SizePct:      1,
		})
	}
	return rules
}

func buildTrailingExitRules(referencePrice string, stopLoss string, trailPct float64, guardActivationBps float64, guardFloorBps float64) []strategy.ExitRule {
	rules := []strategy.ExitRule{}
	if stopLoss != "" {
		rules = append(rules, strategy.ExitRule{
			Type:         strategy.ExitReasonStopLoss,
			Reason:       "strategy stop loss",
			TriggerPrice: stopLoss,
			SizePct:      1,
		})
	}
	if referencePrice != "" && trailPct > 0 {
		rules = append(rules, strategy.ExitRule{
			Type:    strategy.ExitReasonTrailingStop,
			Reason:  "strategy trailing stop",
			SizePct: 1,
			Metadata: map[string]string{
				"trail_pct":                   formatFloat(trailPct),
				"reference_price":             referencePrice,
				"profit_guard_activation_bps": formatFloat(guardActivationBps),
				"profit_guard_floor_bps":      formatFloat(guardFloorBps),
			},
		})
	}
	return rules
}

func buildAdaptiveExitRules(referencePrice string, stopLoss string, params adaptiveExitParameters) []strategy.ExitRule {
	rules := []strategy.ExitRule{}
	if stopLoss != "" {
		rules = append(rules, strategy.ExitRule{
			Type:         strategy.ExitReasonStopLoss,
			Reason:       "adaptive hard risk stop",
			TriggerPrice: stopLoss,
			SizePct:      1,
		})
	}
	if referencePrice == "" || params.trailPct <= 0 {
		return rules
	}
	rules = append(rules, strategy.ExitRule{
		Type:    strategy.ExitReasonTrailingStop,
		Reason:  "adaptive protected trailing stop",
		SizePct: 1,
		Metadata: map[string]string{
			"trail_pct":                   formatFloat(params.trailPct),
			"reference_price":             referencePrice,
			"profit_guard_activation_bps": formatFloat(params.activationBps),
			"profit_guard_floor_bps":      formatFloat(params.floorBps),
			"adaptive_trailing":           "true",
			"runner_activation_bps":       formatFloat(params.runnerActivationBps),
			"runner_trail_pct":            formatFloat(params.runnerTrailPct),
			"volatility_state":            params.volatilityState,
			"atr_pct14":                   formatFloat(params.atrPct),
		},
	})
	return rules
}

func (s *Strategy) adaptiveExitParameters(snapshot strategy.Snapshot, referencePrice float64) adaptiveExitParameters {
	state := normalize(latestSignal(snapshot.Window, "volatility_window_state"))
	atrPct := latestNumeric(snapshot.Window, "atr_pct14")
	if atrPct <= 0 {
		atrPct = latestNumeric(snapshot.Window, "natr14")
	}
	microBps := quoteDistanceBps(s.config.MicroProfitQuote, referencePrice)
	targetBps := quoteDistanceBps(s.config.TargetProfitQuote, referencePrice)
	runnerQuoteBps := quoteDistanceBps(s.config.RunnerProfitQuote, referencePrice)
	floorBps := math.Max(s.config.RoundTripCostBps+s.config.ProfitBufferBps, microBps*0.80)

	activationBps := microBps
	trailPct := clamp(atrPct*adaptiveNormalTrailATRMultiplier, adaptiveMinNormalTrailPct, adaptiveMaxNormalTrailPct)
	switch {
	case signalIs(state, "contracting", "squeeze", "low", "quiet"):
		trailPct = clamp(atrPct*adaptiveLowTrailATRMultiplier, adaptiveMinLowTrailPct, adaptiveMaxLowTrailPct)
	case signalIs(state, "expanding", "expansion", "spike", "high", "breakout", "climax"):
		trailPct = clamp(atrPct*adaptiveExpandingTrailATRMultiplier, adaptiveMinExpandingTrailPct, adaptiveMaxExpandingTrailPct)
	default:
		if state == "" {
			state = "neutral"
		}
	}
	activationBps = math.Max(activationBps, floorBps+2)
	decayActivationBps := math.Max(targetBps, activationBps)
	runnerActivationBps := math.Max(runnerQuoteBps, decayActivationBps)
	runnerTrailPct := clamp(trailPct*adaptiveRunnerTrailMultiplier, trailPct, adaptiveMaxRunnerTrailPct)
	return adaptiveExitParameters{
		volatilityState:     state,
		atrPct:              atrPct,
		activationBps:       activationBps,
		floorBps:            floorBps,
		decayActivationBps:  decayActivationBps,
		runnerActivationBps: runnerActivationBps,
		trailPct:            trailPct,
		runnerTrailPct:      runnerTrailPct,
	}
}

func quoteDistanceBps(quoteDistance float64, referencePrice float64) float64 {
	if quoteDistance <= 0 || referencePrice <= 0 {
		return 0
	}
	return quoteDistance / referencePrice * 10000
}

func clamp(value float64, minimum float64, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func takeProfit(snapshot strategy.Snapshot, key string) string {
	return numericString(snapshot, key)
}

func (s *Strategy) costFlooredTakeProfit(snapshot strategy.Snapshot, side strategy.SignalSide, key string) string {
	structural := takeProfit(snapshot, key)
	if structural == "" || s.config.TakeProfitCostFloorBps <= 0 {
		return structural
	}
	referencePrice, referenceOK := numericValue(snapshot, "close")
	structuralPrice, structuralErr := strconv.ParseFloat(structural, 64)
	if !referenceOK || structuralErr != nil || structuralPrice <= 0 {
		return structural
	}
	distance := referencePrice * s.config.TakeProfitCostFloorBps / 10000
	switch side {
	case strategy.SignalSideBuy:
		return formatFloat(math.Max(structuralPrice, referencePrice+distance))
	case strategy.SignalSideSell:
		return formatFloat(math.Min(structuralPrice, referencePrice-distance))
	default:
		return structural
	}
}

func stopLoss(snapshot strategy.Snapshot, primary string, fallback string) string {
	if value := numericString(snapshot, primary); value != "" {
		return value
	}
	return numericString(snapshot, fallback)
}

func (s *Strategy) boundedStopLoss(snapshot strategy.Snapshot, side strategy.SignalSide, primary string, fallback string) string {
	structural := stopLoss(snapshot, primary, fallback)
	if s.config.MaxStopLossBps <= 0 {
		return structural
	}
	entry, ok := numericValue(snapshot, "close")
	if !ok {
		return structural
	}
	structuralPrice, structuralErr := strconv.ParseFloat(structural, 64)
	structuralOK := structuralErr == nil
	maxDistance := entry * s.config.MaxStopLossBps / 10000
	switch side {
	case strategy.SignalSideBuy:
		limit := entry - maxDistance
		if !structuralOK || structuralPrice <= 0 || structuralPrice >= entry || structuralPrice < limit {
			return formatFloat(limit)
		}
	case strategy.SignalSideSell:
		limit := entry + maxDistance
		if !structuralOK || structuralPrice <= entry || structuralPrice > limit {
			return formatFloat(limit)
		}
	}
	return structural
}

func (s *Strategy) hardRiskStopLoss(snapshot strategy.Snapshot, side strategy.SignalSide) string {
	if s.config.MaxStopLossBps <= 0 {
		return ""
	}
	entry, ok := numericValue(snapshot, "close")
	if !ok {
		return ""
	}
	distance := entry * s.config.MaxStopLossBps / 10000
	switch side {
	case strategy.SignalSideBuy:
		return formatFloat(entry - distance)
	case strategy.SignalSideSell:
		return formatFloat(entry + distance)
	default:
		return ""
	}
}

func numericString(snapshot strategy.Snapshot, key string) string {
	if series, ok := snapshot.Window.Numeric(key); ok && series.Latest > 0 {
		return fmt.Sprintf("%g", series.Latest)
	}
	if value, ok := snapshot.Indicator.Float(key); ok {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	return ""
}

func numericValue(snapshot strategy.Snapshot, key string) (float64, bool) {
	if series, ok := snapshot.Window.Numeric(key); ok && series.Latest > 0 {
		return series.Latest, true
	}
	value, ok := snapshot.Indicator.Float(key)
	return value, ok && value > 0
}

func latestNumeric(window strategy.IndicatorWindowView, key string) float64 {
	series, _ := window.Numeric(key)
	return series.Latest
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func latestSignal(window strategy.IndicatorWindowView, key string) string {
	series, _ := window.Signal(key)
	return series.Latest
}

func latestSignalStableCount(window strategy.IndicatorWindowView, key string) int {
	series, _ := window.Signal(key)
	if series.StableCount > 0 {
		return series.StableCount
	}
	denseSeries, ok := window.Numeric(key + "_win_stable_count")
	if !ok || denseSeries.Latest <= 0 {
		return 0
	}
	return int(denseSeries.Latest)
}

func freshSignalValue(window strategy.IndicatorWindowView, key string, expected string) bool {
	series, ok := window.Signal(key)
	return ok && series.Latest == expected && (series.Changed || series.StableCount == 1)
}

func confirmedTruthySignal(window strategy.IndicatorWindowView, key string) bool {
	series, ok := window.Signal(key)
	if !ok || !truthy(series.Latest) {
		return false
	}
	return latestSignalStableCount(window, key) == 2
}

func truthySignal(window strategy.IndicatorWindowView, key string) bool {
	return truthy(latestSignal(window, key))
}

func truthy(value string) bool {
	switch normalize(value) {
	case "true", "1", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func signalIs(value string, candidates ...string) bool {
	normalized := normalize(value)
	for _, candidate := range candidates {
		if normalized == normalize(candidate) {
			return true
		}
	}
	return false
}

func supportsBias(value string, bias string) bool {
	normalized := normalize(value)
	switch bias {
	case "bull":
		return strings.Contains(normalized, "bull") || strings.Contains(normalized, "long")
	case "bear":
		return strings.Contains(normalized, "bear") || strings.Contains(normalized, "short")
	default:
		return normalized == bias
	}
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func directionForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "up"
	}
	return "down"
}

func oppositeDirectionForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "down"
	}
	return "up"
}

func eventForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "buy"
	}
	return "sell"
}

func biasForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "bull"
	}
	return "bear"
}

func oppositeBias(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "bear"
	}
	return "bull"
}

func oppositeSide(side strategy.SignalSide) strategy.SignalSide {
	if side == strategy.SignalSideBuy {
		return strategy.SignalSideSell
	}
	return strategy.SignalSideBuy
}

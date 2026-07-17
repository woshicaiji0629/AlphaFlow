package supertrend

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

const (
	Name                            = "supertrend"
	ExitModeStructure               = "structure"
	ExitModeTrailing                = "trailing"
	DefaultProfitGuardActivationBps = 150.0
	DefaultProfitGuardFloorBps      = 100.0
	DefaultProfitDecayActivationBps = 150.0
)

type Config struct {
	EntryThreshold           float64
	MaxBlockingTimeframes    int
	MinTakeProfitBps         float64
	MinRewardRiskRatio       float64
	MaxStopLossBps           float64
	ExitMode                 string
	TrailingStopPct          float64
	ProfitGuardActivationBps float64
	ProfitGuardFloorBps      float64
	ProfitDecayActivationBps float64
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
	checks := append(append([]strategy.DiagnosticCheck{}, longDecision.checks...), shortDecision.checks...)
	selected := selectEntry(longDecision, shortDecision, s.config.EntryThreshold)
	if selected.side == strategy.SignalSideHold {
		return s.holdWithChecks(snapshot, strings.Join(append(longDecision.reasons, shortDecision.reasons...), "; "), checks), nil
	}
	rules := s.exitRules(snapshot, selected.side)
	if s.config.ExitMode == ExitModeTrailing && !hasExitRule(rules, strategy.ExitReasonTrailingStop) {
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
	if reversed, values := higherTimeframeReversed(snapshot, opposite); reversed {
		check := diagnosticCheck("structure_invalidation", opposite, true, 0, "10m and 15m confirmed reversal", values)
		return s.signalWithChecks(snapshot, opposite, 1, "10m and 15m confirmed reversal", nil, []strategy.DiagnosticCheck{check})
	}
	profitCheck, profitState := s.profitProtectionCheck(snapshot, currentPosition, holdingSide)
	if profitState == "weak_exit" {
		return s.signalWithChecks(snapshot, opposite, 1, "protected profit momentum decayed", nil, []strategy.DiagnosticCheck{profitCheck})
	}
	decision := s.entry(snapshot, opposite)
	decision = withThresholdCheck(decision, s.config.EntryThreshold)
	if decision.blocked {
		return s.holdWithChecks(snapshot, strings.Join(decision.reasons, "; "), append(decision.checks, profitCheck))
	}
	if !reverseConfirmed(snapshot, opposite) {
		checks := append(decision.checks, diagnosticCheck("reverse_confirmation", opposite, false, 0, "reverse signal not confirmed", nil), profitCheck)
		return s.holdWithChecks(snapshot, "reverse signal not confirmed", checks)
	}
	checks := append(decision.checks, diagnosticCheck("reverse_confirmation", opposite, true, 0, "reverse signal confirmed", nil), profitCheck)
	return s.signalWithChecks(snapshot, opposite, decision.score, strings.Join(decision.reasons, "; "), nil, checks)
}

func (s *Strategy) entry(snapshot strategy.Snapshot, side strategy.SignalSide) entryDecision {
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
	triggerValues := entryTriggerValues(window, side)
	triggered := entryTriggered(window, side)
	standardTriggered := standardEntryTriggered(window, side)
	if !triggered {
		decision.blocked = true
		decision.reasons = append(decision.reasons, fmt.Sprintf("%s trigger missing", side))
		decision.checks = append(decision.checks, diagnosticCheck("entry_trigger", side, false, 0, fmt.Sprintf("%s trigger missing", side), triggerValues))
		return decision
	}
	decision.score = 0.30
	decision.reasons = append(decision.reasons, fmt.Sprintf("%s trigger", side))
	decision.checks = append(decision.checks, diagnosticCheck("entry_trigger", side, true, 0.30, fmt.Sprintf("%s trigger", side), triggerValues))

	energyOK, energyScore, energyReasons, energyValues := momentumEnergy(window, side)
	energyConfirmations, _ := strconv.Atoi(energyValues["confirmations"])
	fakeRisk := fakeRiskLevel(window, side)
	shockOK, shockValues := shockBreakoutAuthorized(snapshot, side, fakeRisk, energyConfirmations)
	regimeOK, regimeValues := higherTimeframeAuthorized(snapshot, side)
	pullbackOK, pullbackValues := pullbackResolved(snapshot, side)
	entryMode := ""
	switch {
	case shockOK:
		entryMode = "shock_breakout"
		decision.score += 0.10
		decision.reasons = append(decision.reasons, "shock breakout authorized")
	case standardTriggered && regimeOK && pullbackOK:
		entryMode = "trend_continuation"
		decision.reasons = append(decision.reasons, "trend continuation authorized")
	default:
		decision.blocked = true
		decision.reasons = append(decision.reasons, "entry mode not authorized")
	}
	modeValues := map[string]string{
		"mode":                            entryMode,
		"shock_authorized":                strconv.FormatBool(shockOK),
		"regime_authorized":               strconv.FormatBool(regimeOK),
		"pullback_resolved":               strconv.FormatBool(pullbackOK),
		"breakout_structure":              shockValues["structure"],
		"shock_structure":                 shockValues["structure"],
		"shock_impulse":                   shockValues["impulse"],
		"shock_impulse_stable_count":      shockValues["impulse_stable_count"],
		"shock_price_volume":              shockValues["price_volume"],
		"shock_volume_expanded":           shockValues["volume_expanded"],
		"shock_3m":                        shockValues["3m"],
		"shock_5m":                        shockValues["5m"],
		"shock_10m":                       shockValues["10m"],
		"shock_15m":                       shockValues["15m"],
		"shock_30m":                       shockValues["30m"],
		"shock_higher_timeframes_clear":   shockValues["higher_timeframes_clear"],
		"shock_higher_timeframes_aligned": shockValues["higher_timeframes_aligned"],
		"shock_fake_risk":                 shockValues["fake_risk"],
		"shock_fake_risk_low":             shockValues["fake_risk_low"],
		"shock_momentum_confirmations":    shockValues["momentum_confirmations"],
		"shock_momentum_required":         shockValues["momentum_required"],
	}
	decision.checks = append(decision.checks, diagnosticCheck("entry_mode", side, entryMode != "", 0.10*boolFloat(shockOK), checkReason(entryMode == "", "entry mode not authorized", entryMode+" authorized"), modeValues))

	fakeBlocked := fakeRisk == "high"
	fakeAccepted := !fakeBlocked
	if !fakeAccepted {
		decision.blocked = true
		decision.reasons = append(decision.reasons, "fake signal risk blocked")
	}
	fakeReason := "fake signal risk accepted"
	decision.checks = append(decision.checks, diagnosticCheck("fake_signal_risk", side, fakeAccepted, 0, checkReason(!fakeAccepted, "high fake signal risk blocked", fakeReason), map[string]string{
		"risk": fakeRisk,
	}))

	regimeAccepted := regimeOK || shockOK
	if !regimeAccepted {
		decision.blocked = true
		decision.reasons = append(decision.reasons, "higher timeframe regime blocked")
	}
	regimeReason := "higher timeframe regime authorized"
	if !regimeOK && shockOK {
		regimeReason = "higher timeframe wait overridden by shock breakout"
	}
	decision.checks = append(decision.checks, diagnosticCheck("higher_timeframe_regime", side, regimeAccepted, 0, checkReason(!regimeAccepted, "higher timeframe regime blocked", regimeReason), regimeValues))

	if !pullbackOK {
		decision.blocked = true
		decision.reasons = append(decision.reasons, "5m pullback not resolved")
	}
	decision.checks = append(decision.checks, diagnosticCheck("pullback_resolution", side, pullbackOK, 0, checkReason(!pullbackOK, "5m pullback not resolved", "5m pullback resolved"), pullbackValues))

	trendOK, trendScore, trendReasons := trendConfirmationForEntry(window, side, shockOK)
	if !trendOK {
		decision.blocked = true
	}
	decision.score += trendScore
	decision.reasons = append(decision.reasons, trendReasons...)
	decision.checks = append(decision.checks, diagnosticCheck("trend", side, trendOK, trendScore, strings.Join(trendReasons, "; "), nil))

	maOK, maScore, maReasons := maConfirmationForEntry(window, side)
	if !maOK {
		decision.blocked = true
	}
	decision.score += maScore
	decision.reasons = append(decision.reasons, maReasons...)
	decision.checks = append(decision.checks, diagnosticCheck("moving_average", side, maOK, maScore, strings.Join(maReasons, "; "), nil))

	macdOK, macdScore, macdReasons := macdConfirmation(window, side)
	if !macdOK && !shockOK {
		decision.blocked = true
	}
	decision.score += macdScore
	decision.reasons = append(decision.reasons, macdReasons...)
	decision.checks = append(decision.checks, entryConfirmationCheck("macd", side, macdOK, !shockOK, macdScore, strings.Join(macdReasons, "; "), nil))

	volumeOK, volumeScore, volumeReasons := volumeConfirmation(window, side)
	if !volumeOK && !shockOK {
		decision.blocked = true
	}
	decision.score += volumeScore
	decision.reasons = append(decision.reasons, volumeReasons...)
	decision.checks = append(decision.checks, entryConfirmationCheck("volume", side, volumeOK, !shockOK, volumeScore, strings.Join(volumeReasons, "; "), nil))

	if !energyOK && !shockOK {
		decision.blocked = true
	}
	decision.score += energyScore
	decision.reasons = append(decision.reasons, energyReasons...)
	decision.checks = append(decision.checks, entryConfirmationCheck("momentum_energy", side, energyOK, !shockOK, energyScore, strings.Join(energyReasons, "; "), energyValues))

	aligned, blocked := classifyTimeframes(snapshot, side)
	shortBlocked := shortTimeframesBlocked(snapshot, side)
	if blocked > s.config.MaxBlockingTimeframes || shortBlocked {
		decision.blocked = true
		decision.reasons = append(decision.reasons, "timeframe blocked")
	}
	if aligned > 0 {
		decision.score += 0.04 * float64(aligned)
		decision.reasons = append(decision.reasons, "timeframes aligned")
	}
	timeframesOK := blocked <= s.config.MaxBlockingTimeframes && !shortBlocked
	decision.checks = append(decision.checks, diagnosticCheck("timeframes", side, timeframesOK, 0.04*float64(aligned), checkReason(!timeframesOK, "timeframe blocked", "timeframes evaluated"), map[string]string{
		"aligned": strconv.Itoa(aligned), "blocked": strconv.Itoa(blocked),
	}))
	return decision
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
	return infoDiagnostic(name, side, reason+"; shock breakout score only", values)
}

func checkReason(blocked bool, blockedReason string, passReason string) string {
	if blocked {
		return blockedReason
	}
	return passReason
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
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
	continuationKey := continuationSignalKey(side)
	if confirmedTruthySignal(window, continuationKey) {
		return true
	}
	return standardEntryTriggered(window, side)
}

func standardEntryTriggered(window strategy.IndicatorWindowView, side strategy.SignalSide) bool {
	if previousWickReclaimed(window, side) {
		return true
	}
	if freshSignalValue(window, "supertrend_direction", directionForSide(side)) {
		return true
	}
	if freshNumericEvent(window, "trend_signal_age") && signalIs(latestSignal(window, "trend_signal_event"), eventForSide(side)) {
		return true
	}
	if freshNumericEvent(window, "ma_window_cross_age") && signalIs(latestSignal(window, "ma_window_cross_event"), maCrossForSide(side)) {
		return true
	}
	return freshNumericEvent(window, "smc_window_bos_age") &&
		truthySignal(window, "smc_window_bos_recent") &&
		latestSignal(window, "smc_window_bias") == biasForSide(side)
}

func entryTriggerValues(window strategy.IndicatorWindowView, side strategy.SignalSide) map[string]string {
	continuationKey := continuationSignalKey(side)
	return map[string]string{
		"shock_impulse_signal":       latestSignal(window, continuationKey),
		"shock_impulse_stable_count": strconv.Itoa(latestSignalStableCount(window, continuationKey)),
		"shock_second_bar_confirmed": strconv.FormatBool(confirmedTruthySignal(window, continuationKey)),
	}
}

func continuationSignalKey(side strategy.SignalSide) string {
	if side == strategy.SignalSideSell {
		return "dump_window_signal"
	}
	return "pump_window_signal"
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

func fakeRiskBlocked(window strategy.IndicatorWindowView, side strategy.SignalSide) bool {
	return fakeRiskLevel(window, side) == "high"
}

func fakeRiskLevel(window strategy.IndicatorWindowView, side strategy.SignalSide) string {
	key := "pump_window_fake_risk"
	if side == strategy.SignalSideSell {
		key = "dump_window_fake_risk"
	}
	return latestSignal(window, key)
}

func shockBreakoutAuthorized(snapshot strategy.Snapshot, side strategy.SignalSide, fakeRisk string, energyConfirmations int) (bool, map[string]string) {
	window := snapshot.Window
	structureConfirmed := signalIs(latestSignal(window, "breakout_window_quality"), "improving", "confirmed") ||
		(truthySignal(window, "smc_window_bos_recent") && latestSignal(window, "smc_window_bias") == biasForSide(side))
	continuationKey := continuationSignalKey(side)
	priceVolume := "confirm_up"
	if side == strategy.SignalSideSell {
		priceVolume = "confirm_down"
	}
	impulseConfirmed := confirmedTruthySignal(window, continuationKey)
	priceVolumeConfirmed := latestSignal(window, "price_volume_confirmation") == priceVolume
	volumeExpanded := signalIs(latestSignal(window, "volume_window_state"),
		"spike", "expanding", "expansion", "breakout", "above_average", "climax")
	localAligned := localTrendAligned(window, side)
	fiveAligned, _ := pullbackResolved(snapshot, side)

	values := map[string]string{
		"structure":              strconv.FormatBool(structureConfirmed),
		"impulse":                strconv.FormatBool(impulseConfirmed),
		"impulse_stable_count":   strconv.Itoa(latestSignalStableCount(window, continuationKey)),
		"price_volume":           strconv.FormatBool(priceVolumeConfirmed),
		"volume_expanded":        strconv.FormatBool(volumeExpanded),
		"3m":                     checkReason(!localAligned, "not_aligned", "aligned"),
		"5m":                     checkReason(!fiveAligned, "not_aligned", "aligned"),
		"10m":                    "missing",
		"15m":                    "missing",
		"30m":                    "missing",
		"fake_risk":              fakeRisk,
		"fake_risk_low":          strconv.FormatBool(fakeRisk == "low"),
		"momentum_confirmations": strconv.Itoa(energyConfirmations),
		"momentum_required":      "3",
	}
	higherTimeframesClear := true
	higherTimeframesAligned := true
	for _, interval := range []string{"10m", "15m", "30m"} {
		timeframe, ok := snapshot.Timeframes[interval]
		if !ok {
			higherTimeframesClear = false
			higherTimeframesAligned = false
			continue
		}
		state := classifyTimeframe(timeframe.Window, side)
		values[interval] = state
		if state == "blocking" || state == "missing" {
			higherTimeframesClear = false
		}
		if state != "aligned" {
			higherTimeframesAligned = false
		}
	}
	values["higher_timeframes_clear"] = strconv.FormatBool(higherTimeframesClear)
	values["higher_timeframes_aligned"] = strconv.FormatBool(higherTimeframesAligned)
	return structureConfirmed && impulseConfirmed && priceVolumeConfirmed && volumeExpanded &&
		localAligned && fiveAligned && higherTimeframesClear && higherTimeframesAligned &&
		fakeRisk == "low" && energyConfirmations >= 3, values
}

func localTrendAligned(window strategy.IndicatorWindowView, side strategy.SignalSide) bool {
	expectedBias := biasForSide(side)
	expectedDirection := directionForSide(side)
	return latestSignal(window, "trend_window_bias") == expectedBias ||
		latestSignal(window, "supertrend_direction") == expectedDirection ||
		latestSignal(window, "alphatrend_direction") == expectedDirection
}

func trendConfirmationForEntry(window strategy.IndicatorWindowView, side strategy.SignalSide, shockBreakout bool) (bool, float64, []string) {
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
		if !shockBreakout {
			return false, 0, []string{"trend quality blocked"}
		}
		return true, 0, []string{"trend compression overridden by shock breakout"}
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
	return values["state"] == "trend", values
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

func (s *Strategy) profitProtectionCheck(
	snapshot strategy.Snapshot,
	currentPosition *strategy.Position,
	holdingSide strategy.SignalSide,
) (strategy.DiagnosticCheck, string) {
	mfeBps, ok := favorableExcursionBps(currentPosition)
	values := map[string]string{
		"state":                  "inactive",
		"mfe_bps":                formatFloat(mfeBps),
		"activation_bps":         formatFloat(s.config.ProfitDecayActivationBps),
		"decay_confirmations":    "0",
		"momentum_confirmations": "0",
		"5m":                     "missing",
		"10m":                    "missing",
		"15m":                    "missing",
	}
	if s.config.ExitMode != ExitModeTrailing || !ok || mfeBps < s.config.ProfitDecayActivationBps {
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
	if side == strategy.SignalSideBuy {
		return buildExitRules(takeProfit(snapshot, "resistance_1"), s.boundedStopLoss(snapshot, side, "supertrend", "support_1"))
	}
	return buildExitRules(takeProfit(snapshot, "support_1"), s.boundedStopLoss(snapshot, side, "supertrend", "resistance_1"))
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

func takeProfit(snapshot strategy.Snapshot, key string) string {
	return numericString(snapshot, key)
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

func freshNumericEvent(window strategy.IndicatorWindowView, key string) bool {
	series, ok := window.Numeric(key)
	return ok && series.Latest == 0
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

func maCrossForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "golden"
	}
	return "dead"
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

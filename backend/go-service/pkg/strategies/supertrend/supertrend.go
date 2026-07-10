package supertrend

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

const Name = "supertrend"

type Config struct {
	EntryThreshold        float64
	MaxBlockingTimeframes int
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
	return s.signalWithChecks(snapshot, selected.side, selected.score, strings.Join(selected.reasons, "; "), exitRules(snapshot, selected.side), checks), nil
}

func (s *Strategy) evaluateExit(snapshot strategy.Snapshot, currentPosition *strategy.Position) strategy.Result {
	var opposite strategy.SignalSide
	switch currentPosition.Side {
	case strategy.PositionSideLong:
		opposite = strategy.SignalSideSell
	case strategy.PositionSideShort:
		opposite = strategy.SignalSideBuy
	default:
		return s.hold(snapshot, "position side not actionable")
	}
	decision := s.entry(snapshot, opposite)
	decision = withThresholdCheck(decision, s.config.EntryThreshold)
	if decision.blocked {
		return s.holdWithChecks(snapshot, strings.Join(decision.reasons, "; "), decision.checks)
	}
	if !reverseConfirmed(snapshot, opposite) {
		checks := append(decision.checks, diagnosticCheck("reverse_confirmation", opposite, false, 0, "reverse signal not confirmed", nil))
		return s.holdWithChecks(snapshot, "reverse signal not confirmed", checks)
	}
	checks := append(decision.checks, diagnosticCheck("reverse_confirmation", opposite, true, 0, "reverse signal confirmed", nil))
	return s.signalWithChecks(snapshot, opposite, decision.score, strings.Join(decision.reasons, "; "), nil, checks)
}

func (s *Strategy) entry(snapshot strategy.Snapshot, side strategy.SignalSide) entryDecision {
	window := snapshot.Window
	decision := entryDecision{
		side:    side,
		reasons: []string{},
	}
	if len(window.Values) == 0 && len(window.Signals) == 0 {
		decision.blocked = true
		decision.reasons = append(decision.reasons, "indicator window missing")
		decision.checks = append(decision.checks, strategy.DiagnosticCheck{Name: "indicator_window", Side: side, Status: strategy.DiagnosticStatusMissing, Reason: "indicator window missing"})
		return decision
	}
	triggered := entryTriggered(window, side)
	if !triggered {
		decision.blocked = true
		decision.reasons = append(decision.reasons, fmt.Sprintf("%s trigger missing", side))
		decision.checks = append(decision.checks, diagnosticCheck("entry_trigger", side, false, 0, fmt.Sprintf("%s trigger missing", side), nil))
		return decision
	}
	decision.score = 0.30
	decision.reasons = append(decision.reasons, fmt.Sprintf("%s trigger", side))
	decision.checks = append(decision.checks, diagnosticCheck("entry_trigger", side, true, 0.30, fmt.Sprintf("%s trigger", side), nil))

	fakeBlocked := fakeRiskBlocked(window, side)
	if fakeBlocked {
		decision.blocked = true
		decision.reasons = append(decision.reasons, "fake signal risk blocked")
	}
	decision.checks = append(decision.checks, diagnosticCheck("fake_signal_risk", side, !fakeBlocked, 0, checkReason(fakeBlocked, "fake signal risk blocked", "fake signal risk accepted"), nil))
	trendOK, trendScore, trendReasons := trendConfirmation(window, side)
	if !trendOK {
		decision.blocked = true
	}
	decision.score += trendScore
	decision.reasons = append(decision.reasons, trendReasons...)
	decision.checks = append(decision.checks, diagnosticCheck("trend", side, trendOK, trendScore, strings.Join(trendReasons, "; "), nil))

	maOK, maScore, maReasons := maConfirmation(window, side)
	if !maOK {
		decision.blocked = true
	}
	decision.score += maScore
	decision.reasons = append(decision.reasons, maReasons...)
	decision.checks = append(decision.checks, diagnosticCheck("moving_average", side, maOK, maScore, strings.Join(maReasons, "; "), nil))

	macdOK, macdScore, macdReasons := macdConfirmation(window, side)
	if !macdOK {
		decision.blocked = true
	}
	decision.score += macdScore
	decision.reasons = append(decision.reasons, macdReasons...)
	decision.checks = append(decision.checks, diagnosticCheck("macd", side, macdOK, macdScore, strings.Join(macdReasons, "; "), nil))

	volumeOK, volumeScore, volumeReasons := volumeConfirmation(window, side)
	if !volumeOK {
		decision.blocked = true
	}
	decision.score += volumeScore
	decision.reasons = append(decision.reasons, volumeReasons...)
	decision.checks = append(decision.checks, diagnosticCheck("volume", side, volumeOK, volumeScore, strings.Join(volumeReasons, "; "), nil))

	aligned, blocked := classifyTimeframes(snapshot, side)
	shortBlocked := shortTimeframesBlocked(snapshot, side)
	if blocked >= s.config.MaxBlockingTimeframes || shortBlocked {
		decision.blocked = true
		decision.reasons = append(decision.reasons, "timeframe blocked")
	}
	if aligned > 0 {
		decision.score += 0.04 * float64(aligned)
		decision.reasons = append(decision.reasons, "timeframes aligned")
	}
	timeframesOK := blocked < s.config.MaxBlockingTimeframes && !shortBlocked
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
	if side == strategy.SignalSideBuy {
		return truthySignal(window, "pump_window_signal") || latestSignal(window, "supertrend_direction") == "up"
	}
	return truthySignal(window, "dump_window_signal") || latestSignal(window, "supertrend_direction") == "down"
}

func fakeRiskBlocked(window strategy.IndicatorWindowView, side strategy.SignalSide) bool {
	key := "pump_window_fake_risk"
	if side == strategy.SignalSideSell {
		key = "dump_window_fake_risk"
	}
	risk := latestSignal(window, key)
	return risk != "" && risk != "low"
}

func trendConfirmation(window strategy.IndicatorWindowView, side strategy.SignalSide) (bool, float64, []string) {
	reasons := []string{}
	if valid := latestSignal(window, "trend_valid"); valid != "" && !truthy(valid) {
		return false, 0, []string{"trend invalid"}
	}
	direction := directionForSide(side)
	if latestSignal(window, "trend_window_bias") == direction ||
		latestSignal(window, "supertrend_direction") == direction ||
		latestSignal(window, "alphatrend_direction") == direction ||
		latestSignal(window, "trend_window_bias") == biasForSide(side) {
		reasons = append(reasons, "trend aligned")
	} else {
		return false, 0, []string{"trend not aligned"}
	}
	progress := latestSignal(window, "trend_price_progress")
	if progress != "" && progress != progressForSide(side) {
		return false, 0, []string{"trend progress blocked"}
	}
	quality := latestSignal(window, "trend_quality")
	if quality == "flat" || quality == "weak" {
		return false, 0, []string{"trend quality blocked"}
	}
	return true, 0.16, reasons
}

func maConfirmation(window strategy.IndicatorWindowView, side strategy.SignalSide) (bool, float64, []string) {
	for _, key := range []string{"ma_ribbon_state", "ma_ribbon_phase"} {
		value := latestSignal(window, key)
		if signalIs(value, "tangled", "flat", "range", "choppy", "compressing") {
			return false, 0, []string{"ma ribbon blocked"}
		}
	}
	expected := biasForSide(side)
	if supportsBias(latestSignal(window, "ma_ribbon_state"), expected) ||
		latestSignal(window, "ma_window_bias") == expected ||
		latestSignal(window, "ema_alignment") == expected {
		return true, 0.14, []string{"ma aligned"}
	}
	return false, 0, []string{"ma not aligned"}
}

func macdConfirmation(window strategy.IndicatorWindowView, side strategy.SignalSide) (bool, float64, []string) {
	expected := biasForSide(side)
	opposite := oppositeBias(side)
	if latestSignal(window, "macd_window_bias") == opposite || latestSignal(window, "macd_momentum") == opposite {
		return false, 0, []string{"macd blocked"}
	}
	divergence := latestSignal(window, "macd_divergence")
	if side == strategy.SignalSideBuy && divergence == "bearish" {
		return false, 0, []string{"macd bearish divergence"}
	}
	if side == strategy.SignalSideSell && divergence == "bullish" {
		return false, 0, []string{"macd bullish divergence"}
	}
	if latestSignal(window, "macd_window_bias") == expected || latestSignal(window, "macd_momentum") == expected {
		return true, 0.14, []string{"macd aligned"}
	}
	return false, 0, []string{"macd not aligned"}
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

func shortTimeframesBlocked(snapshot strategy.Snapshot, side strategy.SignalSide) bool {
	five, fiveOK := snapshot.Timeframes["5m"]
	ten, tenOK := snapshot.Timeframes["10m"]
	return fiveOK && tenOK &&
		classifyTimeframe(five.Window, side) == "blocking" &&
		classifyTimeframe(ten.Window, side) == "blocking"
}

func classifyTimeframe(window strategy.IndicatorWindowView, side strategy.SignalSide) string {
	if len(window.Values) == 0 && len(window.Signals) == 0 {
		return "missing"
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

func exitRules(snapshot strategy.Snapshot, side strategy.SignalSide) []strategy.ExitRule {
	if side == strategy.SignalSideBuy {
		return buildExitRules(takeProfit(snapshot, "resistance_1"), stopLoss(snapshot, "supertrend", "support_1"))
	}
	return buildExitRules(takeProfit(snapshot, "support_1"), stopLoss(snapshot, "supertrend", "resistance_1"))
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

func takeProfit(snapshot strategy.Snapshot, key string) string {
	return numericString(snapshot, key)
}

func stopLoss(snapshot strategy.Snapshot, primary string, fallback string) string {
	if value := numericString(snapshot, primary); value != "" {
		return value
	}
	return numericString(snapshot, fallback)
}

func numericString(snapshot strategy.Snapshot, key string) string {
	if series, ok := snapshot.Window.Values[key]; ok && series.Latest > 0 {
		return fmt.Sprintf("%g", series.Latest)
	}
	if value := snapshot.Indicator.Values[key]; value != "" {
		return value
	}
	return ""
}

func latestSignal(window strategy.IndicatorWindowView, key string) string {
	return window.Signals[key].Latest
}

func latestSignalStableCount(window strategy.IndicatorWindowView, key string) int {
	return window.Signals[key].StableCount
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

func progressForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "advancing"
	}
	return "declining"
}

func oppositeSide(side strategy.SignalSide) strategy.SignalSide {
	if side == strategy.SignalSideBuy {
		return strategy.SignalSideSell
	}
	return strategy.SignalSideBuy
}

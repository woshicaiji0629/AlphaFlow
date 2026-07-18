package position

import (
	"strconv"

	"alphaflow/go-service/pkg/strategy"
)

const (
	defaultMaxPositionSize   = 1.0
	defaultMinOpenConfidence = 0.65
)

type ManagerConfig struct {
	MaxPositionSize      float64
	MarginQuote          float64
	Leverage             float64
	MinOpenConfidence    float64
	DisableShortExposure bool
}

type Manager struct {
	config ManagerConfig
}

func NewManager(config ManagerConfig) *Manager {
	if config.MaxPositionSize <= 0 {
		config.MaxPositionSize = defaultMaxPositionSize
	}
	if config.MinOpenConfidence <= 0 {
		config.MinOpenConfidence = defaultMinOpenConfidence
	}
	return &Manager{config: config}
}

func (m *Manager) Plan(result strategy.Result, currentPosition *strategy.Position) *strategy.OrderPlan {
	return m.PlanWithPrice(result, currentPosition, "")
}

func (m *Manager) PlanWithPrice(
	result strategy.Result,
	currentPosition *strategy.Position,
	currentPrice string,
) *strategy.OrderPlan {
	if exitPlan := m.RiskExit(currentPosition, currentPrice); exitPlan != nil {
		return exitPlan
	}
	current := flatPosition()
	if currentPosition != nil {
		current = *currentPosition
	}
	if result.Signal.Side == strategy.SignalSideHold {
		return holdPlan(current, "no actionable signal")
	}
	if result.Signal.Confidence < m.config.MinOpenConfidence {
		return holdPlan(current, "signal confidence below position threshold")
	}
	switch result.Signal.Side {
	case strategy.SignalSideBuy:
		return m.planLong(result, current)
	case strategy.SignalSideSell:
		return m.planShort(result, current)
	default:
		return holdPlan(current, "unsupported signal side")
	}
}

func (m *Manager) planLong(result strategy.Result, current strategy.Position) *strategy.OrderPlan {
	if current.Side == strategy.PositionSideShort && current.Size > 0 {
		return &strategy.OrderPlan{
			Action:     strategy.PositionActionCloseShort,
			TargetSide: strategy.PositionSideFlat,
			Reason:     result.Signal.Reason,
			ExitReason: strategy.ExitReasonStrategy,
		}
	}
	if current.Side == strategy.PositionSideLong && current.Size > 0 {
		return holdPlan(current, "long position already open")
	}
	return &strategy.OrderPlan{
		Action:     strategy.PositionActionOpenLong,
		TargetSide: strategy.PositionSideLong,
		TargetSize: m.targetSize(result.Signal.Confidence),
		Reason:     result.Signal.Reason,
		ExitRules:  result.ExitRules,
	}
}

func (m *Manager) planShort(result strategy.Result, current strategy.Position) *strategy.OrderPlan {
	if m.config.DisableShortExposure {
		if current.Side == strategy.PositionSideLong && current.Size > 0 {
			return &strategy.OrderPlan{
				Action:     strategy.PositionActionCloseLong,
				TargetSide: strategy.PositionSideFlat,
				Reason:     result.Signal.Reason,
				ExitReason: strategy.ExitReasonStrategy,
			}
		}
		return holdPlan(current, "short exposure disabled")
	}
	if current.Side == strategy.PositionSideLong && current.Size > 0 {
		return &strategy.OrderPlan{
			Action:     strategy.PositionActionCloseLong,
			TargetSide: strategy.PositionSideFlat,
			Reason:     result.Signal.Reason,
			ExitReason: strategy.ExitReasonStrategy,
		}
	}
	if current.Side == strategy.PositionSideShort && current.Size > 0 {
		return holdPlan(current, "short position already open")
	}
	return &strategy.OrderPlan{
		Action:     strategy.PositionActionOpenShort,
		TargetSide: strategy.PositionSideShort,
		TargetSize: m.targetSize(result.Signal.Confidence),
		Reason:     result.Signal.Reason,
		ExitRules:  result.ExitRules,
	}
}

func (m *Manager) targetSize(confidence float64) float64 {
	if confidence <= 0 {
		return 0
	}
	if m.config.MarginQuote > 0 && m.config.Leverage > 0 {
		return m.config.MarginQuote * m.config.Leverage
	}
	return m.config.MaxPositionSize
}

func (m *Manager) RiskExit(currentPosition *strategy.Position, currentPrice string) *strategy.OrderPlan {
	if currentPosition == nil || currentPosition.IsFlat() {
		return nil
	}
	price, ok := parseFloat(currentPrice)
	if !ok {
		return nil
	}
	for index := range currentPosition.ExitRules {
		rule := currentPosition.ExitRules[index]
		if !positionExitRuleTriggered(*currentPosition, price, rule) {
			continue
		}
		return riskExitPlan(currentPosition, rule)
	}
	return nil
}

// RiskExitBar evaluates price-based exit rules against a completed OHLC bar.
// When multiple rules are touched within the same bar, the deterministic
// conservative order is stop loss, trailing stop, then take profit.
func (m *Manager) RiskExitBar(currentPosition *strategy.Position, openValue string, highValue string, lowValue string) (*strategy.OrderPlan, string) {
	if currentPosition == nil || currentPosition.IsFlat() {
		return nil, ""
	}
	high, highOK := parseFloat(highValue)
	low, lowOK := parseFloat(lowValue)
	if !highOK || !lowOK || high <= 0 || low <= 0 || high < low {
		return nil, ""
	}
	open, openOK := parseFloat(openValue)
	for _, ruleType := range []strategy.ExitReasonType{
		strategy.ExitReasonStopLoss,
		strategy.ExitReasonTrailingStop,
		strategy.ExitReasonTakeProfit,
	} {
		for index := range currentPosition.ExitRules {
			rule := currentPosition.ExitRules[index]
			if rule.Type != ruleType {
				continue
			}
			triggerPrice, triggered, gapEligible := barExitTrigger(*currentPosition, &rule, high, low)
			if !triggered {
				continue
			}
			fillPrice := triggerPrice
			if openOK && gapEligible {
				switch currentPosition.Side {
				case strategy.PositionSideLong:
					if open < triggerPrice {
						fillPrice = open
					}
				case strategy.PositionSideShort:
					if open > triggerPrice {
						fillPrice = open
					}
				}
			}
			return riskExitPlan(currentPosition, rule), formatFloat(fillPrice)
		}
	}
	return nil, ""
}

func riskExitPlan(currentPosition *strategy.Position, rule strategy.ExitRule) *strategy.OrderPlan {
	action := closeAction(currentPosition.Side)
	exitSize := exitSize(currentPosition.Size, rule.SizePct)
	if exitSize < currentPosition.Size {
		action = reduceAction(currentPosition.Side)
	}
	return &strategy.OrderPlan{
		Action:        action,
		TargetSide:    strategy.PositionSideFlat,
		Reason:        rule.Reason,
		ExitSize:      exitSize,
		ExitReason:    rule.Type,
		TriggeredRule: &rule,
	}
}

func barExitTrigger(currentPosition strategy.Position, rule *strategy.ExitRule, high float64, low float64) (float64, bool, bool) {
	if rule.Type == strategy.ExitReasonTrailingStop {
		trailPct, ok := parseFloat(rule.Metadata["trail_pct"])
		if !ok || trailPct <= 0 {
			return 0, false, false
		}
		referencePrice, ok := parseFloat(rule.Metadata["reference_price"])
		if !ok || referencePrice <= 0 {
			return 0, false, false
		}
		previousTrigger, previousActive := protectedTrailingTriggerPrice(currentPosition, *rule, referencePrice, trailPct)
		if previousActive && barPriceTouched(currentPosition.Side, rule.Type, high, low, previousTrigger) {
			rule.TriggerPrice = formatFloat(previousTrigger)
			return previousTrigger, true, true
		}
		switch currentPosition.Side {
		case strategy.PositionSideLong:
			if high > referencePrice {
				referencePrice = high
			}
		case strategy.PositionSideShort:
			if low < referencePrice {
				referencePrice = low
			}
		default:
			return 0, false, false
		}
		triggerPrice, active := protectedTrailingTriggerPrice(currentPosition, *rule, referencePrice, trailPct)
		if !active {
			return 0, false, false
		}
		rule.TriggerPrice = formatFloat(triggerPrice)
		metadata := make(map[string]string, len(rule.Metadata))
		for key, value := range rule.Metadata {
			metadata[key] = value
		}
		metadata["reference_price"] = formatFloat(referencePrice)
		rule.Metadata = metadata
		return triggerPrice, barPriceTouched(currentPosition.Side, rule.Type, high, low, triggerPrice), false
	}
	triggerPrice, ok := parseFloat(rule.TriggerPrice)
	if !ok || triggerPrice <= 0 {
		return 0, false, false
	}
	return triggerPrice, barPriceTouched(currentPosition.Side, rule.Type, high, low, triggerPrice), rule.Type == strategy.ExitReasonStopLoss
}

func trailingTriggerPrice(side strategy.PositionSide, referencePrice float64, trailPct float64) float64 {
	if side == strategy.PositionSideLong {
		return referencePrice * (1 - trailPct/100)
	}
	return referencePrice * (1 + trailPct/100)
}

func barPriceTouched(side strategy.PositionSide, ruleType strategy.ExitReasonType, high float64, low float64, triggerPrice float64) bool {
	switch side {
	case strategy.PositionSideLong:
		if isAdverseExit(ruleType) {
			return low <= triggerPrice
		}
		return ruleType == strategy.ExitReasonTakeProfit && high >= triggerPrice
	case strategy.PositionSideShort:
		if isAdverseExit(ruleType) {
			return high >= triggerPrice
		}
		return ruleType == strategy.ExitReasonTakeProfit && low <= triggerPrice
	default:
		return false
	}
}

func isAdverseExit(ruleType strategy.ExitReasonType) bool {
	return ruleType == strategy.ExitReasonStopLoss || ruleType == strategy.ExitReasonTrailingStop
}

func ExitRuleTriggered(side strategy.PositionSide, price float64, rule strategy.ExitRule) bool {
	if rule.Type == strategy.ExitReasonTrailingStop {
		return trailingStopTriggered(side, price, rule)
	}
	triggerPrice, ok := parseFloat(rule.TriggerPrice)
	if !ok {
		return false
	}
	switch side {
	case strategy.PositionSideLong:
		return (rule.Type == strategy.ExitReasonTakeProfit && price >= triggerPrice) ||
			(rule.Type == strategy.ExitReasonStopLoss && price <= triggerPrice)
	case strategy.PositionSideShort:
		return (rule.Type == strategy.ExitReasonTakeProfit && price <= triggerPrice) ||
			(rule.Type == strategy.ExitReasonStopLoss && price >= triggerPrice)
	default:
		return false
	}
}

func positionExitRuleTriggered(currentPosition strategy.Position, price float64, rule strategy.ExitRule) bool {
	if rule.Type != strategy.ExitReasonTrailingStop {
		return ExitRuleTriggered(currentPosition.Side, price, rule)
	}
	trailPct, ok := parseFloat(rule.Metadata["trail_pct"])
	if !ok || trailPct <= 0 {
		return false
	}
	referencePrice, ok := parseFloat(rule.Metadata["reference_price"])
	if !ok || referencePrice <= 0 {
		return false
	}
	triggerPrice, active := protectedTrailingTriggerPrice(currentPosition, rule, referencePrice, trailPct)
	if !active {
		return false
	}
	return barPriceTouched(currentPosition.Side, rule.Type, price, price, triggerPrice)
}

func protectedTrailingTriggerPrice(
	currentPosition strategy.Position,
	rule strategy.ExitRule,
	referencePrice float64,
	trailPct float64,
) (float64, bool) {
	activationBps, activationOK := parseFloat(rule.Metadata["profit_guard_activation_bps"])
	floorBps, floorOK := parseFloat(rule.Metadata["profit_guard_floor_bps"])
	if !activationOK || !floorOK || activationBps <= 0 || floorBps <= 0 {
		return trailingTriggerPrice(currentPosition.Side, referencePrice, trailPct), true
	}
	entryPrice, ok := parseFloat(currentPosition.EntryPrice)
	if !ok || entryPrice <= 0 {
		return 0, false
	}

	trailTrigger := protectedTrailPrice(currentPosition, rule, entryPrice, referencePrice, trailPct)
	switch currentPosition.Side {
	case strategy.PositionSideLong:
		if referencePrice < entryPrice*(1+activationBps/10000) {
			return 0, false
		}
		profitFloor := entryPrice * (1 + floorBps/10000)
		if profitFloor > trailTrigger {
			return profitFloor, true
		}
		return trailTrigger, true
	case strategy.PositionSideShort:
		if referencePrice > entryPrice*(1-activationBps/10000) {
			return 0, false
		}
		profitFloor := entryPrice * (1 - floorBps/10000)
		if profitFloor < trailTrigger {
			return profitFloor, true
		}
		return trailTrigger, true
	default:
		return 0, false
	}
}

func protectedTrailPrice(
	currentPosition strategy.Position,
	rule strategy.ExitRule,
	entryPrice float64,
	referencePrice float64,
	baseTrailPct float64,
) float64 {
	baseTrigger := trailingTriggerPrice(currentPosition.Side, referencePrice, baseTrailPct)
	if rule.Metadata["adaptive_trailing"] != "true" {
		return baseTrigger
	}
	runnerActivationBps, activationOK := parseFloat(rule.Metadata["runner_activation_bps"])
	runnerTrailPct, trailOK := parseFloat(rule.Metadata["runner_trail_pct"])
	if !activationOK || !trailOK || runnerActivationBps <= 0 || runnerTrailPct <= baseTrailPct {
		return baseTrigger
	}
	switch currentPosition.Side {
	case strategy.PositionSideLong:
		runnerReference := entryPrice * (1 + runnerActivationBps/10000)
		if referencePrice < runnerReference {
			return baseTrigger
		}
		anchor := trailingTriggerPrice(currentPosition.Side, runnerReference, baseTrailPct)
		runnerTrigger := trailingTriggerPrice(currentPosition.Side, referencePrice, runnerTrailPct)
		if anchor > runnerTrigger {
			return anchor
		}
		return runnerTrigger
	case strategy.PositionSideShort:
		runnerReference := entryPrice * (1 - runnerActivationBps/10000)
		if referencePrice > runnerReference {
			return baseTrigger
		}
		anchor := trailingTriggerPrice(currentPosition.Side, runnerReference, baseTrailPct)
		runnerTrigger := trailingTriggerPrice(currentPosition.Side, referencePrice, runnerTrailPct)
		if anchor < runnerTrigger {
			return anchor
		}
		return runnerTrigger
	default:
		return baseTrigger
	}
}

func RefreshPositionExtremes(currentPosition strategy.Position, price string) strategy.Position {
	return RefreshPositionExtremesAt(currentPosition, price, 0)
}

func RefreshPositionExtremesAt(currentPosition strategy.Position, price string, barOpenTime int64) strategy.Position {
	value, ok := parseFloat(price)
	if !ok {
		return currentPosition
	}
	highest := value
	highestTime := barOpenTime
	if current, ok := parseFloat(currentPosition.HighestPrice); ok && current >= highest {
		highest = current
		if current > value || currentPosition.HighestPriceBarOpenTime > 0 {
			highestTime = currentPosition.HighestPriceBarOpenTime
		}
	}
	lowest := value
	lowestTime := barOpenTime
	if current, ok := parseFloat(currentPosition.LowestPrice); ok && current <= lowest {
		lowest = current
		if current < value || currentPosition.LowestPriceBarOpenTime > 0 {
			lowestTime = currentPosition.LowestPriceBarOpenTime
		}
	}
	currentPosition.HighestPrice = formatFloat(highest)
	currentPosition.LowestPrice = formatFloat(lowest)
	currentPosition.HighestPriceBarOpenTime = highestTime
	currentPosition.LowestPriceBarOpenTime = lowestTime
	for index := range currentPosition.ExitRules {
		currentPosition.ExitRules[index] = refreshExitRule(currentPosition.Side, currentPosition.ExitRules[index], highest, lowest)
	}
	return currentPosition
}

func RefreshPositionBarExtremes(currentPosition strategy.Position, high string, low string) strategy.Position {
	return RefreshPositionBarExtremesAt(currentPosition, high, low, 0)
}

func RefreshPositionBarExtremesAt(currentPosition strategy.Position, high string, low string, barOpenTime int64) strategy.Position {
	currentPosition = RefreshPositionExtremesAt(currentPosition, high, barOpenTime)
	return RefreshPositionExtremesAt(currentPosition, low, barOpenTime)
}

func flatPosition() strategy.Position {
	return strategy.Position{Side: strategy.PositionSideFlat}
}

func holdPlan(currentPosition strategy.Position, reason string) *strategy.OrderPlan {
	return &strategy.OrderPlan{
		Action:     strategy.PositionActionHold,
		TargetSide: currentPosition.Side,
		TargetSize: currentPosition.Size,
		Reason:     reason,
	}
}

func closeAction(side strategy.PositionSide) strategy.PositionAction {
	if side == strategy.PositionSideShort {
		return strategy.PositionActionCloseShort
	}
	return strategy.PositionActionCloseLong
}

func reduceAction(side strategy.PositionSide) strategy.PositionAction {
	if side == strategy.PositionSideShort {
		return strategy.PositionActionReduceShort
	}
	return strategy.PositionActionReduceLong
}

func exitSize(positionSize float64, sizePct float64) float64 {
	if sizePct <= 0 {
		sizePct = 1
	}
	if sizePct > 1 {
		sizePct = 1
	}
	return positionSize * sizePct
}

func trailingStopTriggered(side strategy.PositionSide, price float64, rule strategy.ExitRule) bool {
	trailPct, ok := parseFloat(rule.Metadata["trail_pct"])
	if !ok || trailPct <= 0 {
		return false
	}
	referencePrice, ok := parseFloat(rule.Metadata["reference_price"])
	if !ok || referencePrice <= 0 {
		return false
	}
	distance := referencePrice * trailPct / 100
	switch side {
	case strategy.PositionSideLong:
		return price <= referencePrice-distance
	case strategy.PositionSideShort:
		return price >= referencePrice+distance
	default:
		return false
	}
}

func refreshExitRule(side strategy.PositionSide, rule strategy.ExitRule, highest float64, lowest float64) strategy.ExitRule {
	if rule.Type != strategy.ExitReasonTrailingStop {
		return rule
	}
	reference := highest
	if side == strategy.PositionSideShort {
		reference = lowest
	}
	metadata := make(map[string]string, len(rule.Metadata)+1)
	for key, value := range rule.Metadata {
		metadata[key] = value
	}
	metadata["reference_price"] = formatFloat(reference)
	rule.Metadata = metadata
	return rule
}

func parseFloat(value string) (float64, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

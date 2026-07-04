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
		if !ExitRuleTriggered(currentPosition.Side, price, rule) {
			continue
		}
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
	return nil
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

func RefreshPositionExtremes(currentPosition strategy.Position, price string) strategy.Position {
	value, ok := parseFloat(price)
	if !ok {
		return currentPosition
	}
	highest := value
	if current, ok := parseFloat(currentPosition.HighestPrice); ok && current > highest {
		highest = current
	}
	lowest := value
	if current, ok := parseFloat(currentPosition.LowestPrice); ok && current < lowest {
		lowest = current
	}
	currentPosition.HighestPrice = formatFloat(highest)
	currentPosition.LowestPrice = formatFloat(lowest)
	for index := range currentPosition.ExitRules {
		currentPosition.ExitRules[index] = refreshExitRule(currentPosition.Side, currentPosition.ExitRules[index], highest, lowest)
	}
	return currentPosition
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

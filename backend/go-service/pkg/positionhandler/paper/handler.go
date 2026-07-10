package paper

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyroute"
	"alphaflow/go-service/pkg/symbolspec"
)

type FeeConfig struct {
	FeeRate   float64
	RebatePct float64
}

type SizingConfig struct {
	MarginQuote  float64
	Leverage     float64
	Capabilities map[symbolspec.Key]symbolspec.Capability
}

type Options struct {
	PositionManager *position.Manager
	PositionStore   position.Store
	EventStore      position.EventStore
	IntentStore     execution.IntentStore
	Broker          execution.Broker
	FeeConfig       FeeConfig
	SizingConfig    SizingConfig
	Now             func() int64
}

type Handler struct {
	positionManager *position.Manager
	positionStore   position.Store
	eventStore      position.EventStore
	intentStore     execution.IntentStore
	broker          execution.Broker
	feeConfig       FeeConfig
	sizingConfig    SizingConfig
	now             func() int64
}

func New(options Options) (*Handler, error) {
	if options.PositionManager == nil {
		return nil, fmt.Errorf("position manager is required")
	}
	if options.PositionStore == nil {
		return nil, fmt.Errorf("position store is required")
	}
	if options.Now == nil {
		options.Now = func() int64 { return 0 }
	}
	return &Handler{
		positionManager: options.PositionManager,
		positionStore:   options.PositionStore,
		eventStore:      options.EventStore,
		intentStore:     options.IntentStore,
		broker:          options.Broker,
		feeConfig:       options.FeeConfig,
		sizingConfig:    options.SizingConfig,
		now:             options.Now,
	}, nil
}

func (h *Handler) HandleResult(ctx context.Context, input strategy.Context, result strategy.Result, route strategyroute.Route) error {
	if input.Target.Scope != strategy.PositionScopePaper && input.Target.Scope != strategy.PositionScopeBacktest {
		return nil
	}
	now := h.now()
	currentPosition := input.Positions[result.StrategyName]
	price := currentPrice(input)
	refreshedPosition, err := h.refreshLocalPosition(ctx, input.Target, result.StrategyName, currentPosition, price)
	if err != nil {
		return err
	}
	currentPosition = refreshedPosition
	if err := h.appendEvent(ctx, signalEvent(input.Target, result, now)); err != nil {
		return err
	}
	plan := h.positionManager.PlanWithPrice(result, currentPosition, price)
	orderPlan, err := h.orderPlanWithQuantity(input.Target, *plan, price)
	if err != nil {
		return fmt.Errorf("build order quantity for strategy %s: %w", result.StrategyName, err)
	}
	intent, ok, err := execution.BuildOrderIntent(execution.IntentRequest{
		Target:         input.Target,
		StrategyName:   result.StrategyName,
		Plan:           orderPlan,
		Position:       currentPosition,
		BarOpenTime:    result.Signal.OpenTime,
		ReferencePrice: price,
		CreatedAt:      now,
	})
	if err != nil {
		return fmt.Errorf("build order intent for strategy %s: %w", result.StrategyName, err)
	}
	if !ok {
		return nil
	}
	if err := h.appendEvent(ctx, orderIntentEvent(input.Target, result, intent, plan, now)); err != nil {
		return err
	}
	if h.broker == nil {
		return nil
	}
	report, alreadyApplied, err := h.executeRecoverable(ctx, intent)
	if err != nil {
		return fmt.Errorf("execute order intent %s: %w", intent.IntentID, err)
	}
	if alreadyApplied {
		return nil
	}
	if err := h.appendEvent(ctx, orderFilledEvent(input.Target, result, currentPosition, intent, report, plan, h.feeConfig, h.sizingConfig, now)); err != nil {
		return err
	}
	if err := h.applyLocalFill(ctx, input.Target, result, currentPosition, intent, report, plan); err != nil {
		return fmt.Errorf("apply local fill for strategy %s: %w", result.StrategyName, err)
	}
	if err := h.saveIntentState(ctx, intent, report, execution.IntentStatePositionApplied); err != nil {
		return err
	}
	if err := h.saveIntentState(ctx, intent, report, execution.IntentStateCompleted); err != nil {
		return err
	}
	return nil
}

func (h *Handler) executeRecoverable(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	if h.intentStore == nil {
		report, err := h.broker.Execute(ctx, intent)
		return report, false, err
	}
	record, err := h.intentStore.GetIntent(ctx, intent.IntentID)
	if err != nil {
		return execution.ExecutionReport{}, false, err
	}
	if record != nil {
		switch record.State {
		case execution.IntentStateCompleted, execution.IntentStatePositionApplied:
			return record.Report, true, nil
		case execution.IntentStateFilled, execution.IntentStateRejected:
			return record.Report, false, nil
		case execution.IntentStateSubmitted:
			recoverable, ok := h.broker.(execution.RecoverableBroker)
			if !ok {
				return execution.ExecutionReport{}, false, fmt.Errorf("broker cannot recover submitted intent")
			}
			report, found, err := recoverable.Recover(ctx, intent)
			if err != nil {
				return execution.ExecutionReport{}, false, err
			}
			if !found {
				return execution.ExecutionReport{}, false, fmt.Errorf("submitted intent outcome is unknown")
			}
			if err := h.saveReportState(ctx, intent, report); err != nil {
				return execution.ExecutionReport{}, false, err
			}
			return report, false, nil
		}
	}
	if err := h.saveIntentState(ctx, intent, execution.ExecutionReport{}, execution.IntentStateCreated); err != nil {
		return execution.ExecutionReport{}, false, err
	}
	if err := h.saveIntentState(ctx, intent, execution.ExecutionReport{}, execution.IntentStateSubmitted); err != nil {
		return execution.ExecutionReport{}, false, err
	}
	report, err := h.broker.Execute(ctx, intent)
	if err != nil {
		return execution.ExecutionReport{}, false, err
	}
	if err := h.saveReportState(ctx, intent, report); err != nil {
		return execution.ExecutionReport{}, false, err
	}
	return report, false, nil
}

func (h *Handler) saveReportState(ctx context.Context, intent execution.OrderIntent, report execution.ExecutionReport) error {
	state := execution.IntentStateRejected
	if report.Status == execution.ExecutionStatusFilled {
		state = execution.IntentStateFilled
	}
	return h.saveIntentState(ctx, intent, report, state)
}

func (h *Handler) saveIntentState(ctx context.Context, intent execution.OrderIntent, report execution.ExecutionReport, state execution.IntentState) error {
	if h.intentStore == nil {
		return nil
	}
	if err := h.intentStore.SaveIntent(ctx, execution.IntentRecord{Intent: intent, Report: report, State: state, UpdatedAt: h.now()}); err != nil {
		return fmt.Errorf("save intent %s state %s: %w", intent.IntentID, state, err)
	}
	return nil
}

func (h *Handler) appendEvent(ctx context.Context, event strategy.StrategyEvent) error {
	if h.eventStore == nil {
		return nil
	}
	if err := h.eventStore.AppendEvent(ctx, event); err != nil {
		return fmt.Errorf("append strategy event %s: %w", event.EventType, err)
	}
	return nil
}

func currentPrice(input strategy.Context) string {
	snapshot, ok := input.Snapshots[input.Target.Interval]
	if !ok {
		return ""
	}
	if snapshot.Price.LastPrice != "" {
		return snapshot.Price.LastPrice
	}
	if snapshot.Price.MarkPrice != "" {
		return snapshot.Price.MarkPrice
	}
	return snapshot.Current.Close
}

func (h *Handler) orderPlanWithQuantity(target strategy.Target, plan strategy.OrderPlan, price string) (strategy.OrderPlan, error) {
	switch plan.Action {
	case strategy.PositionActionOpenLong, strategy.PositionActionOpenShort:
		if plan.TargetSize <= 0 {
			return plan, nil
		}
		if h.sizingConfig.MarginQuote <= 0 || h.sizingConfig.Leverage <= 0 {
			return plan, nil
		}
		value, ok := parseFloat(price)
		if !ok || value <= 0 {
			return strategy.OrderPlan{}, fmt.Errorf("price is required to convert quote notional to quantity")
		}
		capability, ok := h.sizingConfig.Capabilities[symbolspec.NewKey(target.Exchange, target.Market, target.Symbol)]
		if ok {
			order, err := symbolspec.NormalizeQuoteOrder(capability, value, plan.TargetSize)
			if err != nil {
				return strategy.OrderPlan{}, err
			}
			plan.TargetSize = order.Quantity
			return plan, nil
		}
		plan.TargetSize = plan.TargetSize / value
	}
	return plan, nil
}

func (h *Handler) refreshLocalPosition(
	ctx context.Context,
	target strategy.Target,
	strategyName string,
	currentPosition *strategy.Position,
	price string,
) (*strategy.Position, error) {
	if currentPosition == nil || currentPosition.IsFlat() || price == "" {
		return currentPosition, nil
	}
	if target.Scope != strategy.PositionScopeBacktest && target.Scope != strategy.PositionScopePaper {
		return currentPosition, nil
	}
	refreshed := position.RefreshPositionExtremes(*currentPosition, price)
	refreshed.UpdatedAt = h.now()
	if err := h.positionStore.SavePosition(ctx, refreshed); err != nil {
		return nil, fmt.Errorf("refresh position extremes for strategy %s: %w", strategyName, err)
	}
	return &refreshed, nil
}

func (h *Handler) applyLocalFill(
	ctx context.Context,
	target strategy.Target,
	result strategy.Result,
	currentPosition *strategy.Position,
	intent execution.OrderIntent,
	report execution.ExecutionReport,
	plan *strategy.OrderPlan,
) error {
	if target.Scope != strategy.PositionScopeBacktest && target.Scope != strategy.PositionScopePaper {
		return nil
	}
	if report.Status != execution.ExecutionStatusFilled {
		return nil
	}
	switch intent.Action {
	case execution.OrderActionOpen:
		return h.positionStore.SavePosition(ctx, strategy.Position{
			Scope:        target.Scope,
			RunID:        target.RunID,
			Exchange:     target.Exchange,
			Market:       target.Market,
			Symbol:       target.Symbol,
			Account:      target.Account,
			StrategyName: result.StrategyName,
			Mode:         strategy.ExchangePositionModeNet,
			PositionSide: strategy.ExchangePositionSide(intent.PositionSide),
			Side:         positionSideFromIntent(intent),
			Size:         report.FilledQuantity,
			InitialSize:  report.FilledQuantity,
			EntryPrice:   report.AveragePrice,
			HighestPrice: report.AveragePrice,
			LowestPrice:  report.AveragePrice,
			ExitRules:    result.ExitRules,
			EntryTime:    report.UpdatedAt,
			EntryReason:  intent.Reason,
			UpdatedAt:    report.UpdatedAt,
		})
	case execution.OrderActionClose:
		return h.positionStore.DeletePosition(ctx, positionKey(target, result.StrategyName))
	case execution.OrderActionReduce:
		if currentPosition == nil {
			return nil
		}
		remaining := currentPosition.Size - report.FilledQuantity
		if remaining <= 0 {
			return h.positionStore.DeletePosition(ctx, positionKey(target, result.StrategyName))
		}
		updated := *currentPosition
		updated.Size = remaining
		updated.UpdatedAt = report.UpdatedAt
		if plan != nil && plan.TriggeredRule != nil {
			updated.ExitRules = removeExitRule(updated.ExitRules, *plan.TriggeredRule)
		}
		return h.positionStore.SavePosition(ctx, updated)
	default:
		return nil
	}
}

func positionKey(target strategy.Target, strategyName string) position.Key {
	return position.Key{
		Scope:        target.Scope,
		RunID:        target.RunID,
		Account:      target.Account,
		Exchange:     target.Exchange,
		Market:       target.Market,
		Symbol:       target.Symbol,
		StrategyName: strategyName,
		PositionSide: strategy.ExchangePositionSideNet,
	}
}

func positionSideFromIntent(intent execution.OrderIntent) strategy.PositionSide {
	if intent.PositionSide == string(strategy.ExchangePositionSideShort) {
		return strategy.PositionSideShort
	}
	return strategy.PositionSideLong
}

func signalEvent(target strategy.Target, result strategy.Result, now int64) strategy.StrategyEvent {
	return baseEvent(target, result, strategy.EventTypeSignalGenerated, now, result.Signal.OpenTime, "").
		withSignal(result)
}

func orderIntentEvent(
	target strategy.Target,
	result strategy.Result,
	intent execution.OrderIntent,
	plan *strategy.OrderPlan,
	now int64,
) strategy.StrategyEvent {
	event := baseEvent(target, result, strategy.EventTypeOrderIntentCreated, now, result.Signal.OpenTime, intent.IntentID).
		withSignal(result)
	event.IntentID = intent.IntentID
	event.PositionSide = strategy.ExchangePositionSide(intent.PositionSide)
	event.Size = intent.Quantity
	event.Reason = intent.Reason
	event.Metadata = exitMetadata(plan, nil)
	return event
}

func orderFilledEvent(
	target strategy.Target,
	result strategy.Result,
	currentPosition *strategy.Position,
	intent execution.OrderIntent,
	report execution.ExecutionReport,
	plan *strategy.OrderPlan,
	feeConfig FeeConfig,
	sizingConfig SizingConfig,
	now int64,
) strategy.StrategyEvent {
	eventTime := report.UpdatedAt
	if eventTime == 0 {
		eventTime = now
	}
	event := baseEvent(target, result, strategy.EventTypeOrderFilled, eventTime, result.Signal.OpenTime, report.ExchangeOrderID).
		withSignal(result)
	event.IntentID = intent.IntentID
	event.ExchangeOrderID = report.ExchangeOrderID
	event.PositionSide = strategy.ExchangePositionSide(intent.PositionSide)
	event.Size = report.FilledQuantity
	event.Price = report.AveragePrice
	feeConfig = localFeeConfig(target.Scope, feeConfig)
	sizingConfig = localSizingConfig(target.Scope, sizingConfig)
	metrics := fillMetrics(currentPosition, intent, report, feeConfig, sizingConfig)
	event.Notional = formatFloat(metrics.Notional)
	event.Fee = formatFloat(metrics.Fee)
	event.PnL = formatFloat(metrics.NetPnL)
	event.Reason = fillReason(report, plan)
	event.Metadata = exitMetadata(plan, metrics)
	if report.Error != "" {
		if event.Metadata == nil {
			event.Metadata = map[string]string{}
		}
		event.Metadata["error"] = report.Error
	}
	return event
}

func localFeeConfig(scope strategy.PositionScope, feeConfig FeeConfig) FeeConfig {
	if scope != strategy.PositionScopeBacktest && scope != strategy.PositionScopePaper {
		return FeeConfig{}
	}
	return feeConfig
}

func localSizingConfig(scope strategy.PositionScope, sizingConfig SizingConfig) SizingConfig {
	if scope != strategy.PositionScopeBacktest && scope != strategy.PositionScopePaper {
		return SizingConfig{}
	}
	return sizingConfig
}

type Metrics struct {
	Notional          float64
	Fee               float64
	GrossFee          float64
	Rebate            float64
	FeeRate           float64
	RebatePct         float64
	MarginQuote       float64
	Leverage          float64
	GrossPnL          float64
	NetPnL            float64
	ReturnPct         float64
	ReturnOnMarginPct float64
}

func fillMetrics(
	currentPosition *strategy.Position,
	intent execution.OrderIntent,
	report execution.ExecutionReport,
	feeConfig FeeConfig,
	sizingConfig SizingConfig,
) *Metrics {
	price, ok := parseFloat(report.AveragePrice)
	if !ok || report.FilledQuantity <= 0 {
		return &Metrics{}
	}
	notional := price * report.FilledQuantity
	fee := report.Fee
	grossFee := fee
	rebate := 0.0
	if fee <= 0 && feeConfig.FeeRate > 0 {
		grossFee = notional * feeConfig.FeeRate
		rebate = grossFee * normalizedRebatePct(feeConfig.RebatePct) / 100
		fee = grossFee - rebate
		if fee < 0 {
			fee = 0
		}
	}
	metrics := &Metrics{
		Notional:    notional,
		Fee:         fee,
		GrossFee:    grossFee,
		Rebate:      rebate,
		FeeRate:     feeConfig.FeeRate,
		RebatePct:   normalizedRebatePct(feeConfig.RebatePct),
		MarginQuote: sizingConfig.MarginQuote,
		Leverage:    sizingConfig.Leverage,
	}
	if currentPosition == nil || intent.Action == execution.OrderActionOpen {
		metrics.NetPnL = -fee
		return metrics
	}
	entryPrice, ok := parseFloat(currentPosition.EntryPrice)
	if !ok || entryPrice <= 0 {
		metrics.NetPnL = -fee
		return metrics
	}
	entryNotional := entryPrice * report.FilledQuantity
	entryFee := 0.0
	entryGrossFee := 0.0
	entryRebate := 0.0
	if feeConfig.FeeRate > 0 {
		entryGrossFee = entryNotional * feeConfig.FeeRate
		entryRebate = entryGrossFee * normalizedRebatePct(feeConfig.RebatePct) / 100
		entryFee = entryGrossFee - entryRebate
		if entryFee < 0 {
			entryFee = 0
		}
	}
	if currentPosition.Side == strategy.PositionSideShort {
		metrics.GrossPnL = (entryPrice - price) * report.FilledQuantity
	} else {
		metrics.GrossPnL = (price - entryPrice) * report.FilledQuantity
	}
	metrics.Fee += entryFee
	metrics.GrossFee += entryGrossFee
	metrics.Rebate += entryRebate
	metrics.NetPnL = metrics.GrossPnL - metrics.Fee
	if entryNotional > 0 {
		metrics.ReturnPct = metrics.NetPnL / entryNotional * 100
	}
	if sizingConfig.MarginQuote > 0 {
		metrics.ReturnOnMarginPct = metrics.NetPnL / sizingConfig.MarginQuote * 100
	} else if sizingConfig.Leverage > 0 && entryNotional > 0 {
		metrics.ReturnOnMarginPct = metrics.NetPnL / (entryNotional / sizingConfig.Leverage) * 100
	}
	return metrics
}

func fillReason(report execution.ExecutionReport, plan *strategy.OrderPlan) string {
	if plan != nil && plan.ExitReason != "" {
		return string(plan.ExitReason)
	}
	if plan != nil && plan.Reason != "" {
		return plan.Reason
	}
	return string(report.Status)
}

func exitMetadata(plan *strategy.OrderPlan, metrics *Metrics) map[string]string {
	metadata := map[string]string{}
	if plan != nil {
		if plan.ExitReason != "" {
			metadata["exit_reason"] = string(plan.ExitReason)
		}
		if plan.Reason != "" {
			metadata["rule_reason"] = plan.Reason
		}
		if plan.ExitSize > 0 {
			metadata["exit_size"] = formatFloat(plan.ExitSize)
		}
		if plan.TriggeredRule != nil {
			metadata["trigger_price"] = plan.TriggeredRule.TriggerPrice
			metadata["size_pct"] = formatFloat(plan.TriggeredRule.SizePct)
			if plan.TriggeredRule.Reason != "" {
				metadata["rule_reason"] = plan.TriggeredRule.Reason
			}
		}
	}
	if metrics != nil {
		metadata["gross_pnl"] = formatFloat(metrics.GrossPnL)
		metadata["return_pct"] = formatFloat(metrics.ReturnPct)
		metadata["return_on_margin_pct"] = formatFloat(metrics.ReturnOnMarginPct)
		metadata["gross_fee"] = formatFloat(metrics.GrossFee)
		metadata["rebate"] = formatFloat(metrics.Rebate)
		metadata["fee_rate"] = formatFloat(metrics.FeeRate)
		metadata["rebate_pct"] = formatFloat(metrics.RebatePct)
		metadata["margin_quote"] = formatFloat(metrics.MarginQuote)
		metadata["leverage"] = formatFloat(metrics.Leverage)
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func normalizedRebatePct(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func removeExitRule(rules []strategy.ExitRule, triggered strategy.ExitRule) []strategy.ExitRule {
	for index, rule := range rules {
		if exitRuleEqual(rule, triggered) {
			return append(append([]strategy.ExitRule{}, rules[:index]...), rules[index+1:]...)
		}
	}
	return rules
}

func exitRuleEqual(left strategy.ExitRule, right strategy.ExitRule) bool {
	return left.Type == right.Type &&
		left.Reason == right.Reason &&
		left.TriggerPrice == right.TriggerPrice &&
		left.SizePct == right.SizePct
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
	value = math.Round(value*1e12) / 1e12
	return strconv.FormatFloat(value, 'f', -1, 64)
}

type eventBuilder strategy.StrategyEvent

func baseEvent(
	target strategy.Target,
	result strategy.Result,
	eventType strategy.EventType,
	eventTime int64,
	barOpenTime int64,
	unique string,
) eventBuilder {
	eventID := strings.Join([]string{
		string(target.Scope),
		target.RunID,
		target.Exchange,
		target.Market,
		target.Symbol,
		result.StrategyName,
		string(eventType),
		fmt.Sprintf("%d", barOpenTime),
		unique,
	}, ":")
	return eventBuilder{
		EventID:      eventID,
		Scope:        target.Scope,
		RunID:        target.RunID,
		Account:      target.Account,
		Exchange:     target.Exchange,
		Market:       target.Market,
		Symbol:       target.Symbol,
		StrategyName: result.StrategyName,
		EventType:    eventType,
		EventTime:    eventTime,
		BarOpenTime:  barOpenTime,
		CreatedAt:    eventTime,
	}
}

func (b eventBuilder) withSignal(result strategy.Result) strategy.StrategyEvent {
	event := strategy.StrategyEvent(b)
	event.Side = result.Signal.Side
	event.Score = result.Signal.Score
	event.Confidence = result.Signal.Confidence
	event.Reason = result.Signal.Reason
	if result.Analysis.Summary != "" || len(result.Analysis.Checks) > 0 {
		encoded, err := json.Marshal(result.Analysis)
		if err != nil {
			event.Metadata = map[string]string{"analysis_error": err.Error()}
		} else {
			event.Metadata = map[string]string{"analysis": string(encoded)}
		}
	}
	return event
}

var _ strategyroute.ResultHandler = (*Handler)(nil)

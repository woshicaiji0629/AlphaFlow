package execution

import (
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

type IntentRequest struct {
	IntentID       string
	IdempotencyKey string
	Target         strategy.Target
	StrategyName   string
	Plan           strategy.OrderPlan
	Position       *strategy.Position
	BarOpenTime    int64
	ReferencePrice string
	CreatedAt      int64
}

func BuildOrderIntent(request IntentRequest) (OrderIntent, bool, error) {
	if request.Plan.Action == strategy.PositionActionHold {
		return OrderIntent{}, false, nil
	}
	action, side, positionSide, reduceOnly, err := mapPlanAction(request.Plan.Action)
	if err != nil {
		return OrderIntent{}, false, err
	}
	quantity := intentQuantity(request.Plan, request.Position)
	if quantity <= 0 {
		return OrderIntent{}, false, fmt.Errorf("intent quantity must be positive")
	}
	idempotencyKey := request.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = IntentIdempotencyKey(request, side, positionSide)
	}
	intentID := request.IntentID
	if intentID == "" {
		intentID = idempotencyKey
	}
	intent := OrderIntent{
		IntentID:       intentID,
		IdempotencyKey: idempotencyKey,
		StrategyName:   request.StrategyName,
		Scope:          string(request.Target.Scope),
		Exchange:       request.Target.Exchange,
		Account:        request.Target.Account,
		RunID:          request.Target.RunID,
		Market:         request.Target.Market,
		Symbol:         request.Target.Symbol,
		PositionSide:   string(positionSide),
		Action:         action,
		Side:           side,
		Type:           OrderTypeMarket,
		Quantity:       quantity,
		ReferencePrice: request.ReferencePrice,
		ReduceOnly:     reduceOnly,
		Reason:         request.Plan.Reason,
		BarOpenTime:    request.BarOpenTime,
		ExitRules:      append([]strategy.ExitRule(nil), request.Plan.ExitRules...),
		TriggeredRule:  request.Plan.TriggeredRule,
		CreatedAt:      request.CreatedAt,
	}
	return intent, true, nil
}

func IntentIdempotencyKey(
	request IntentRequest,
	side OrderSide,
	positionSide strategy.ExchangePositionSide,
) string {
	return strings.Join([]string{
		"intent",
		string(request.Target.Scope),
		request.Target.RunID,
		request.Target.Account,
		request.Target.Exchange,
		request.Target.Market,
		request.Target.Symbol,
		request.StrategyName,
		strconv.FormatInt(request.DecisionOpenTime(), 10),
		string(request.Plan.Action),
		string(side),
		string(positionSide),
	}, ":")
}

func (request IntentRequest) DecisionOpenTime() int64 {
	if request.BarOpenTime > 0 {
		return request.BarOpenTime
	}
	if request.CreatedAt > 0 {
		return request.CreatedAt
	}
	return 0
}

func mapPlanAction(action strategy.PositionAction) (OrderAction, OrderSide, strategy.ExchangePositionSide, bool, error) {
	switch action {
	case strategy.PositionActionOpenLong:
		return OrderActionOpen, OrderSideBuy, strategy.ExchangePositionSideLong, false, nil
	case strategy.PositionActionOpenShort:
		return OrderActionOpen, OrderSideSell, strategy.ExchangePositionSideShort, false, nil
	case strategy.PositionActionCloseLong:
		return OrderActionClose, OrderSideSell, strategy.ExchangePositionSideLong, true, nil
	case strategy.PositionActionCloseShort:
		return OrderActionClose, OrderSideBuy, strategy.ExchangePositionSideShort, true, nil
	case strategy.PositionActionReduceLong:
		return OrderActionReduce, OrderSideSell, strategy.ExchangePositionSideLong, true, nil
	case strategy.PositionActionReduceShort:
		return OrderActionReduce, OrderSideBuy, strategy.ExchangePositionSideShort, true, nil
	default:
		return "", "", "", false, fmt.Errorf("unsupported position action %q", action)
	}
}

func intentQuantity(plan strategy.OrderPlan, position *strategy.Position) float64 {
	switch plan.Action {
	case strategy.PositionActionOpenLong, strategy.PositionActionOpenShort:
		return plan.TargetSize
	case strategy.PositionActionCloseLong,
		strategy.PositionActionCloseShort,
		strategy.PositionActionReduceLong,
		strategy.PositionActionReduceShort:
		if plan.ExitSize > 0 {
			return plan.ExitSize
		}
		if position != nil {
			return position.Size
		}
	}
	return 0
}

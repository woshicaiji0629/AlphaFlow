package app

import (
	"context"
	"fmt"
	"strings"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionadapter"
)

type brokerRouter struct {
	brokers map[string]executionadapter.Adapter
}

func newBrokerRouter() *brokerRouter {
	return &brokerRouter{brokers: map[string]executionadapter.Adapter{}}
}
func brokerKey(exchange, account string) string {
	return strings.ToLower(strings.TrimSpace(exchange)) + ":" + strings.TrimSpace(account)
}
func (r *brokerRouter) add(exchange, account string, adapter executionadapter.Adapter) error {
	key := brokerKey(exchange, account)
	if key == ":" || adapter == nil {
		return fmt.Errorf("exchange, account and adapter are required")
	}
	if _, ok := r.brokers[key]; ok {
		return fmt.Errorf("duplicate execution account %s", key)
	}
	r.brokers[key] = adapter
	return nil
}
func (r *brokerRouter) adapter(intent execution.OrderIntent) (executionadapter.Adapter, error) {
	a := r.brokers[brokerKey(intent.Exchange, intent.Account)]
	if a == nil {
		return nil, fmt.Errorf("execution account %s:%s is not configured", intent.Exchange, intent.Account)
	}
	return a, nil
}
func (r *brokerRouter) Execute(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, error) {
	a, err := r.adapter(intent)
	if err != nil {
		return execution.ExecutionReport{}, err
	}
	return a.Execute(ctx, intent)
}
func (r *brokerRouter) Recover(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	a, err := r.adapter(intent)
	if err != nil {
		return execution.ExecutionReport{}, false, err
	}
	return a.Recover(ctx, intent)
}
func (r *brokerRouter) ClientOrderID(intent execution.OrderIntent) (string, error) {
	a, err := r.adapter(intent)
	if err != nil {
		return "", err
	}
	identifier, ok := a.(executionadapter.ClientOrderIdentifier)
	if !ok {
		return "", fmt.Errorf("exchange %s does not expose client order ids", intent.Exchange)
	}
	return identifier.ClientOrderID(intent.IntentID), nil
}

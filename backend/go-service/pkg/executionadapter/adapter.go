package executionadapter

import (
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
	"alphaflow/go-service/pkg/strategy"
	"context"
)

type Adapter interface {
	execution.RecoverableBroker
	TestConnection(context.Context) error
	Account(context.Context) (execution.AccountSnapshot, error)
	Positions(context.Context) ([]strategy.Position, error)
	OpenOrders(context.Context, string) ([]execution.ExchangeOrder, error)
	Capability(context.Context, string) (execution.SymbolCapability, error)
	CancelOrder(context.Context, string, string) error
}
type Factory func(executionaccount.Account, executionaccount.Credential) (Adapter, error)

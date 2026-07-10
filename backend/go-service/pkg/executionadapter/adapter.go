package executionadapter

import (
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
	"alphaflow/go-service/pkg/strategy"
	"context"
)

type PrivateEventType string

const (
	PrivateEventOrder    PrivateEventType = "order"
	PrivateEventFill     PrivateEventType = "fill"
	PrivateEventPosition PrivateEventType = "position"
	PrivateEventAccount  PrivateEventType = "account"
)

type PrivateEvent struct {
	Type      PrivateEventType
	Exchange  string
	Account   string
	Sequence  int64
	UpdatedAt int64
	Order     *execution.ExchangeOrder
	Report    *execution.ExecutionReport
	Position  *strategy.Position
	Snapshot  *execution.AccountSnapshot
}

type PrivateEventSink func(context.Context, PrivateEvent) error

type PrivateStreamer interface {
	StreamPrivate(context.Context, PrivateEventSink) error
}

type ClientOrderIdentifier interface{ ClientOrderID(string) string }

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

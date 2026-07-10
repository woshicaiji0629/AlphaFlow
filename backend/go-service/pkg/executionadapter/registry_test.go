package executionadapter

import (
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
	"alphaflow/go-service/pkg/strategy"
	"context"
	"testing"
)

func TestRegistryBuildsRegisteredAdapter(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("custom", func(executionaccount.Account, executionaccount.Credential) (Adapter, error) {
		return fakeAdapter{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Build(executionaccount.Account{ID: "a", Exchange: "custom", Environment: executionaccount.EnvironmentTestnet}, executionaccount.Credential{APIKey: "k", APISecret: "s"})
	if err != nil || got == nil {
		t.Fatalf("Build()=%#v,%v", got, err)
	}
}

type fakeAdapter struct{}

func (fakeAdapter) Execute(context.Context, execution.OrderIntent) (execution.ExecutionReport, error) {
	return execution.ExecutionReport{}, nil
}
func (fakeAdapter) Recover(context.Context, execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	return execution.ExecutionReport{}, false, nil
}
func (fakeAdapter) TestConnection(context.Context) error { return nil }
func (fakeAdapter) Account(context.Context) (execution.AccountSnapshot, error) {
	return execution.AccountSnapshot{}, nil
}
func (fakeAdapter) Positions(context.Context) ([]strategy.Position, error) { return nil, nil }
func (fakeAdapter) OpenOrders(context.Context, string) ([]execution.ExchangeOrder, error) {
	return nil, nil
}
func (fakeAdapter) Capability(context.Context, string) (execution.SymbolCapability, error) {
	return execution.SymbolCapability{}, nil
}
func (fakeAdapter) CancelOrder(context.Context, string, string) error { return nil }

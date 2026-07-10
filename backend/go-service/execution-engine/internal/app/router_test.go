package app

import (
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionadapter"
	"context"
	"testing"
)

type fakeAdapter struct{ executionadapter.Adapter }

func (f fakeAdapter) Execute(context.Context, execution.OrderIntent) (execution.ExecutionReport, error) {
	return execution.ExecutionReport{ExchangeOrderID: "o1"}, nil
}
func (f fakeAdapter) Recover(context.Context, execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	return execution.ExecutionReport{}, false, nil
}
func TestBrokerRouterRoutesByExchangeAndAccount(t *testing.T) {
	r := newBrokerRouter()
	if err := r.add("weex", "a", fakeAdapter{}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Execute(context.Background(), execution.OrderIntent{Exchange: "WEEX", Account: "a"})
	if err != nil || got.ExchangeOrderID != "o1" {
		t.Fatalf("report=%#v err=%v", got, err)
	}
	if _, err := r.Execute(context.Background(), execution.OrderIntent{Exchange: "weex", Account: "missing"}); err == nil {
		t.Fatal("missing account error=nil")
	}
}

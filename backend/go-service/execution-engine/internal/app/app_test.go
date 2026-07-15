package app

import (
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionbus"
	"context"
	"testing"
)

func TestProcessExecutesAndPublishesPaperReport(t *testing.T) {
	b := &fakeBus{}
	store := execution.NewMemoryIntentStore()
	intent := execution.OrderIntent{IntentID: "intent-1", Type: execution.OrderTypeMarket, Quantity: 1, ReferencePrice: "100"}
	if err := process(context.Background(), b, store, execution.NewPaperBroker("", func() int64 { return 2000 }), executionbus.IntentMessage{ID: intent.IntentID, Intent: intent}); err != nil {
		t.Fatal(err)
	}
	if len(b.reports) != 1 || b.reports[0].Status != execution.ExecutionStatusFilled {
		t.Fatalf("reports=%#v", b.reports)
	}
	record, _ := store.GetIntent(context.Background(), intent.IntentID)
	if record == nil || record.State != execution.IntentStateCompleted {
		t.Fatalf("record=%#v", record)
	}
}
func TestProcessRecoversSubmittedIntent(t *testing.T) {
	b := &fakeBus{}
	store := execution.NewMemoryIntentStore()
	intent := execution.OrderIntent{IntentID: "intent-2"}
	_ = store.SaveIntent(context.Background(), execution.IntentRecord{Intent: intent, State: execution.IntentStateSubmitted})
	broker := recoverBroker{}
	if err := process(context.Background(), b, store, broker, executionbus.IntentMessage{ID: intent.IntentID, Intent: intent}); err != nil {
		t.Fatal(err)
	}
	if len(b.reports) != 1 || b.reports[0].ExchangeOrderID != "recovered" {
		t.Fatalf("reports=%#v", b.reports)
	}
}
func TestProcessKeepsAcceptedExchangeOrderSubmitted(t *testing.T) {
	b := &fakeBus{}
	store := execution.NewMemoryIntentStore()
	intent := execution.OrderIntent{IntentID: "intent-3"}
	if err := process(context.Background(), b, store, acceptedBroker{}, executionbus.IntentMessage{ID: intent.IntentID, Intent: intent}); err != nil {
		t.Fatal(err)
	}
	record, _ := store.GetIntent(context.Background(), intent.IntentID)
	if record.State != execution.IntentStateSubmitted {
		t.Fatalf("record=%#v", record)
	}
}

func TestShouldDeadLetterAtConfiguredDeliveryLimit(t *testing.T) {
	if shouldDeadLetter(executionbus.IntentMessage{Delivery: 4}, 5) {
		t.Fatal("delivery below limit must remain retryable")
	}
	if !shouldDeadLetter(executionbus.IntentMessage{Delivery: 5}, 5) {
		t.Fatal("delivery at limit must be dead-lettered")
	}
}

type acceptedBroker struct{}

func (acceptedBroker) Execute(context.Context, execution.OrderIntent) (execution.ExecutionReport, error) {
	return execution.ExecutionReport{Status: execution.ExecutionStatusAccepted, UpdatedAt: 3}, nil
}

type recoverBroker struct{}

func (recoverBroker) Execute(context.Context, execution.OrderIntent) (execution.ExecutionReport, error) {
	return execution.ExecutionReport{}, nil
}
func (recoverBroker) Recover(context.Context, execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	return execution.ExecutionReport{ExchangeOrderID: "recovered", Status: execution.ExecutionStatusFilled, UpdatedAt: 2}, true, nil
}

type fakeBus struct{ reports []execution.ExecutionReport }

func (*fakeBus) ReadIntents(context.Context) ([]executionbus.IntentMessage, error) { return nil, nil }
func (b *fakeBus) PublishReport(_ context.Context, r execution.ExecutionReport) error {
	b.reports = append(b.reports, r)
	return nil
}
func (*fakeBus) Ack(context.Context, executionbus.IntentMessage) error { return nil }
func (*fakeBus) DeadLetterIntent(context.Context, executionbus.IntentMessage, string) error {
	return nil
}
func (*fakeBus) PublishIntent(context.Context, execution.OrderIntent) error { return nil }

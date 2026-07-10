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

type fakeBus struct{ reports []execution.ExecutionReport }

func (*fakeBus) ReadIntents(context.Context) ([]executionbus.IntentMessage, error) { return nil, nil }
func (b *fakeBus) PublishReport(_ context.Context, r execution.ExecutionReport) error {
	b.reports = append(b.reports, r)
	return nil
}
func (*fakeBus) Ack(context.Context, executionbus.IntentMessage) error { return nil }

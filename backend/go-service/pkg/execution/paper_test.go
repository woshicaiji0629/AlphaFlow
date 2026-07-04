package execution

import (
	"context"
	"testing"
)

func TestPaperBrokerFillsMarketOrder(t *testing.T) {
	broker := NewPaperBroker("100.5", func() int64 { return 123 })

	report, err := broker.Execute(context.Background(), OrderIntent{
		IntentID: "intent-1",
		Type:     OrderTypeMarket,
		Quantity: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != ExecutionStatusFilled {
		t.Fatalf("status = %q, want %q", report.Status, ExecutionStatusFilled)
	}
	if report.FilledQuantity != 2 {
		t.Fatalf("filled quantity = %v, want 2", report.FilledQuantity)
	}
	if report.AveragePrice != "100.5" {
		t.Fatalf("average price = %q, want 100.5", report.AveragePrice)
	}
	if report.UpdatedAt != 123 {
		t.Fatalf("updated at = %d, want 123", report.UpdatedAt)
	}
}

func TestPaperBrokerRejectsLimitOrder(t *testing.T) {
	broker := NewPaperBroker("100.5", nil)

	report, err := broker.Execute(context.Background(), OrderIntent{
		IntentID: "intent-1",
		Type:     OrderTypeLimit,
		Quantity: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != ExecutionStatusRejected {
		t.Fatalf("status = %q, want %q", report.Status, ExecutionStatusRejected)
	}
}

func TestPaperBrokerRejectsInvalidQuantity(t *testing.T) {
	broker := NewPaperBroker("100.5", nil)

	report, err := broker.Execute(context.Background(), OrderIntent{
		IntentID: "intent-1",
		Type:     OrderTypeMarket,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != ExecutionStatusRejected {
		t.Fatalf("status = %q, want %q", report.Status, ExecutionStatusRejected)
	}
}

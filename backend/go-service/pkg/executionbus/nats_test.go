package executionbus

import (
	"testing"

	"alphaflow/go-service/pkg/execution"
)

func TestReportMessageIDDistinguishesPartialFillUpdates(t *testing.T) {
	first := execution.ExecutionReport{
		IntentID:       "intent-1",
		Status:         execution.ExecutionStatusPartial,
		FilledQuantity: 0.25,
		UpdatedAt:      1000,
	}
	second := first
	second.FilledQuantity = 0.5
	second.UpdatedAt = 1001

	if reportMessageID(first) == reportMessageID(second) {
		t.Fatal("partial fill updates must use different NATS message IDs")
	}
}

func TestReportMessageIDIsStableForSameReport(t *testing.T) {
	report := execution.ExecutionReport{
		IntentID:       "intent-1",
		Status:         execution.ExecutionStatusFilled,
		FilledQuantity: 1,
		UpdatedAt:      1000,
	}

	if reportMessageID(report) != reportMessageID(report) {
		t.Fatal("the same report must use a stable NATS message ID")
	}
}

func TestDeadLetterMessageIDUsesStreamSequenceWhenIntentIDMissing(t *testing.T) {
	first := deadLetterMessageID(IntentMessage{StreamSequence: 10})
	second := deadLetterMessageID(IntentMessage{StreamSequence: 11})
	if first == second {
		t.Fatalf("missing intent IDs must not collapse to one dead-letter ID: %q", first)
	}
}

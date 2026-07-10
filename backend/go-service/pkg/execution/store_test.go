package execution

import (
	"context"
	"testing"
)

func TestMemoryIntentStoreRoundTrip(t *testing.T) {
	store := NewMemoryIntentStore()
	record := IntentRecord{
		Intent: OrderIntent{IntentID: "intent-1"},
		Report: ExecutionReport{IntentID: "intent-1", Status: ExecutionStatusFilled},
		State:  IntentStateFilled,
	}
	if err := store.SaveIntent(context.Background(), record); err != nil {
		t.Fatalf("SaveIntent() error = %v", err)
	}
	got, err := store.GetIntent(context.Background(), "intent-1")
	if err != nil {
		t.Fatalf("GetIntent() error = %v", err)
	}
	if got == nil || got.State != IntentStateFilled || got.Report.Status != ExecutionStatusFilled {
		t.Fatalf("record = %#v, want filled", got)
	}
}

func TestRedisIntentStoreRequiresIntentID(t *testing.T) {
	store := NewRedisIntentStore(nil, "")
	if err := store.SaveIntent(context.Background(), IntentRecord{}); err != nil {
		t.Fatalf("nil-client SaveIntent() error = %v", err)
	}
	if got, err := store.GetIntent(context.Background(), "missing"); err != nil || got != nil {
		t.Fatalf("nil-client GetIntent() = %#v, %v; want nil, nil", got, err)
	}
}

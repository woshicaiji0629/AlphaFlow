package store

import (
	"context"
	"testing"
	"time"
)

func TestSplitClaimedRecordsGroupsByKind(t *testing.T) {
	claims := []claimedPendingRecord{
		{payload: "kline-1", record: pendingClickHouseRecord{Kind: pendingKindKline}},
		{payload: "kline-2", record: pendingClickHouseRecord{Kind: pendingKindKline}},
	}

	batch, err := splitClaimedRecords(claims)
	if err != nil {
		t.Fatalf("splitClaimedRecords: %v", err)
	}
	if len(batch.klineClaims) != 2 {
		t.Fatalf("kline claims = %d, want 2", len(batch.klineClaims))
	}
	if batch.klineClaims[0].payload != "kline-1" || batch.klineClaims[1].payload != "kline-2" {
		t.Fatalf("kline claim order = %#v, want kline payload order preserved", batch.klineClaims)
	}
}

func TestSplitClaimedRecordsSkipsLegacyIndicatorKind(t *testing.T) {
	batch, err := splitClaimedRecords([]claimedPendingRecord{
		{payload: "indicator-1", record: pendingClickHouseRecord{Kind: "indicator"}},
	})
	if err != nil {
		t.Fatalf("splitClaimedRecords: %v", err)
	}
	if len(batch.klineClaims) != 0 {
		t.Fatalf("kline claims = %d, want 0", len(batch.klineClaims))
	}
}

func TestSplitClaimedRecordsRejectsUnsupportedKind(t *testing.T) {
	_, err := splitClaimedRecords([]claimedPendingRecord{
		{payload: "bad", record: pendingClickHouseRecord{Kind: "bad"}},
	})
	if err == nil {
		t.Fatal("expected unsupported kind error")
	}
}

func TestWaitPendingFlushDebounceCanBeCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startedAt := time.Now()
	if err := waitPendingFlushDebounce(ctx, time.Second); err == nil {
		t.Fatal("expected canceled context error")
	}
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("debounce cancel elapsed = %s, want under 100ms", elapsed)
	}
}

func TestShouldContinuePendingFlush(t *testing.T) {
	if !shouldContinuePendingFlush(100, 100) {
		t.Fatal("expected full batch to continue flushing")
	}
	if shouldContinuePendingFlush(99, 100) {
		t.Fatal("expected partial batch to stop flushing")
	}
	if !shouldContinuePendingFlush(100, 0) {
		t.Fatal("expected default full batch to continue flushing")
	}
}

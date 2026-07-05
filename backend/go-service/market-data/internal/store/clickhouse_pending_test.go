package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/model"
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
	if len(batch.ackOnlyClaims) != 1 {
		t.Fatalf("ack only claims = %d, want 1", len(batch.ackOnlyClaims))
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

func TestPendingWriterPublishesKlineRecords(t *testing.T) {
	queue := &fakePendingQueue{}
	writer := NewClickHousePendingWriter(queue, &fakePendingClickHouseWriter{}, PendingWriterOptions{})
	writeErr := errors.New("clickhouse unavailable")
	if err := writer.EnqueueKlines(context.Background(), []model.Kline{testPendingKline()}, writeErr); err != nil {
		t.Fatalf("EnqueueKlines() error = %v", err)
	}
	if len(queue.published) != 1 {
		t.Fatalf("published records = %d, want 1", len(queue.published))
	}
	record, err := decodePendingRecord(string(queue.published[0]))
	if err != nil {
		t.Fatalf("decode published record: %v", err)
	}
	if record.Kind != pendingKindKline {
		t.Fatalf("kind = %q, want kline", record.Kind)
	}
	if record.LastError != writeErr.Error() {
		t.Fatalf("last error = %q, want %q", record.LastError, writeErr.Error())
	}
}

func TestPendingWriterFlushAcksSuccessfulKlines(t *testing.T) {
	payload := pendingPayload(t, pendingClickHouseRecord{Kind: pendingKindKline, Kline: testPendingKline()})
	queue := &fakePendingQueue{
		messages: []pendingQueueMessage{{ID: "1", Payload: payload, DeliveryCount: 1}},
	}
	clickhouse := &fakePendingClickHouseWriter{}
	writer := NewClickHousePendingWriter(queue, clickhouse, PendingWriterOptions{RetryBatch: 10})

	count, err := writer.Flush(context.Background())
	if err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("flushed count = %d, want 1", count)
	}
	if len(clickhouse.writtenKlines) != 1 {
		t.Fatalf("written klines = %d, want 1", len(clickhouse.writtenKlines))
	}
	if len(queue.acked) != 1 || queue.acked[0].ID != "1" {
		t.Fatalf("acked = %#v, want message 1", queue.acked)
	}
}

func TestPendingWriterDeadLettersMaxDeliveredFailures(t *testing.T) {
	payload := pendingPayload(t, pendingClickHouseRecord{Kind: pendingKindKline, Kline: testPendingKline()})
	queue := &fakePendingQueue{
		messages: []pendingQueueMessage{{ID: "1", Payload: payload, DeliveryCount: 5}},
	}
	clickhouse := &fakePendingClickHouseWriter{writeKlinesErr: errors.New("still unavailable")}
	writer := NewClickHousePendingWriter(queue, clickhouse, PendingWriterOptions{
		RetryBatch:    10,
		MaxDeliveries: 5,
	})

	count, err := writer.Flush(context.Background())
	if err == nil {
		t.Fatal("Flush() error = nil, want write error")
	}
	if count != 0 {
		t.Fatalf("flushed count = %d, want 0", count)
	}
	if len(queue.deadLetters) != 1 || queue.deadLetters[0].ID != "1" {
		t.Fatalf("dead letters = %#v, want message 1", queue.deadLetters)
	}
	if len(queue.acked) != 1 || queue.acked[0].ID != "1" {
		t.Fatalf("acked = %#v, want message 1", queue.acked)
	}
}

func TestNormalizeNATSPendingQueueOptionsDefaults(t *testing.T) {
	options := normalizeNATSPendingQueueOptions(NATSPendingQueueOptions{})
	if options.Stream != DefaultClickHousePendingStream {
		t.Fatalf("stream = %q, want %q", options.Stream, DefaultClickHousePendingStream)
	}
	if options.Subject != DefaultClickHousePendingSubject {
		t.Fatalf("subject = %q, want %q", options.Subject, DefaultClickHousePendingSubject)
	}
	if options.Durable != DefaultClickHousePendingDurable {
		t.Fatalf("durable = %q, want %q", options.Durable, DefaultClickHousePendingDurable)
	}
	if options.AckWait <= 0 {
		t.Fatalf("ack wait = %s, want positive", options.AckWait)
	}
	if options.MaxDeliveries != 5 {
		t.Fatalf("max deliveries = %d, want 5", options.MaxDeliveries)
	}
	if options.MaxPending != 100000 {
		t.Fatalf("max pending = %d, want 100000", options.MaxPending)
	}
	if options.DeadLetterSubject != DefaultClickHousePendingDeadLetterSubject {
		t.Fatalf("dead letter subject = %q, want %q", options.DeadLetterSubject, DefaultClickHousePendingDeadLetterSubject)
	}
}

func pendingPayload(t *testing.T, record pendingClickHouseRecord) []byte {
	t.Helper()
	payload, err := jsonMarshalPendingRecord(record)
	if err != nil {
		t.Fatalf("marshal pending record: %v", err)
	}
	return payload
}

func jsonMarshalPendingRecord(record pendingClickHouseRecord) ([]byte, error) {
	return json.Marshal(record)
}

func testPendingKline() model.Kline {
	return model.Kline{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1,
		IsClosed: true,
	}
}

type fakePendingQueue struct {
	published   [][]byte
	messages    []pendingQueueMessage
	acked       []pendingQueueMessage
	deadLetters []pendingQueueMessage
}

func (q *fakePendingQueue) Publish(ctx context.Context, payloads [][]byte) error {
	q.published = append(q.published, payloads...)
	return ctx.Err()
}

func (q *fakePendingQueue) Fetch(ctx context.Context, batch int, maxWait time.Duration) ([]pendingQueueMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(q.messages) == 0 {
		return nil, nil
	}
	count := len(q.messages)
	if batch > 0 && count > batch {
		count = batch
	}
	messages := append([]pendingQueueMessage(nil), q.messages[:count]...)
	q.messages = q.messages[count:]
	return messages, nil
}

func (q *fakePendingQueue) Ack(ctx context.Context, messages []pendingQueueMessage) error {
	q.acked = append(q.acked, messages...)
	return ctx.Err()
}

func (q *fakePendingQueue) DeadLetter(ctx context.Context, message pendingQueueMessage, reason string) error {
	q.deadLetters = append(q.deadLetters, message)
	return ctx.Err()
}

func (q *fakePendingQueue) Close() error {
	return nil
}

type fakePendingClickHouseWriter struct {
	writtenKlines  []model.Kline
	writeKlinesErr error
}

func (w *fakePendingClickHouseWriter) WriteKline(ctx context.Context, kline model.Kline) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.writtenKlines = append(w.writtenKlines, kline)
	return w.writeKlinesErr
}

func (w *fakePendingClickHouseWriter) WriteKlines(ctx context.Context, klines []model.Kline) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.writeKlinesErr != nil {
		return w.writeKlinesErr
	}
	w.writtenKlines = append(w.writtenKlines, klines...)
	return nil
}

func (w *fakePendingClickHouseWriter) Close() error {
	return nil
}

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

const (
	pendingKindKline     = "kline"
	pendingFlushDebounce = 50 * time.Millisecond
)

type PendingWriterOptions struct {
	RetryInterval time.Duration
	RetryBatch    int
	MaxDeliveries int
}

type pendingQueue interface {
	Publish(ctx context.Context, payloads [][]byte) error
	Fetch(ctx context.Context, batch int, maxWait time.Duration) ([]pendingQueueMessage, error)
	Ack(ctx context.Context, messages []pendingQueueMessage) error
	DeadLetter(ctx context.Context, message pendingQueueMessage, reason string) error
	Close() error
}

type pendingQueueMessage struct {
	ID            string
	Payload       []byte
	DeliveryCount int64
	raw           any
}

type ClickHousePendingWriter struct {
	queue      pendingQueue
	clickhouse clickHouseWriter
	options    PendingWriterOptions
	wake       chan struct{}
}

type pendingClickHouseRecord struct {
	Kind      string                  `json:"kind"`
	Kline     model.Kline             `json:"kline,omitempty"`
	Indicator model.IndicatorSnapshot `json:"indicator,omitempty"`
	Attempts  int                     `json:"attempts"`
	LastError string                  `json:"last_error,omitempty"`
	UpdatedAt int64                   `json:"updated_at"`
}

type claimedPendingRecord struct {
	message pendingQueueMessage
	payload string
	record  pendingClickHouseRecord
}

type pendingRecordBatch struct {
	klineClaims   []claimedPendingRecord
	ackOnlyClaims []claimedPendingRecord
}

func NewClickHousePendingWriter(
	queue pendingQueue,
	clickhouse clickHouseWriter,
	options PendingWriterOptions,
) *ClickHousePendingWriter {
	if options.RetryInterval <= 0 {
		options.RetryInterval = 10 * time.Second
	}
	if options.RetryBatch <= 0 {
		options.RetryBatch = 100
	}
	if options.MaxDeliveries <= 0 {
		options.MaxDeliveries = 5
	}
	return &ClickHousePendingWriter{
		queue:      queue,
		clickhouse: clickhouse,
		options:    options,
		wake:       make(chan struct{}, 1),
	}
}

func (w *ClickHousePendingWriter) EnqueueKline(ctx context.Context, kline model.Kline, writeErr error) error {
	return w.enqueueMany(ctx, []pendingClickHouseRecord{{
		Kind:      pendingKindKline,
		Kline:     kline,
		Attempts:  1,
		LastError: errorString(writeErr),
		UpdatedAt: time.Now().UnixMilli(),
	}})
}

func (w *ClickHousePendingWriter) EnqueueKlines(ctx context.Context, klines []model.Kline, writeErr error) error {
	if len(klines) == 0 {
		return nil
	}
	updatedAt := time.Now().UnixMilli()
	records := make([]pendingClickHouseRecord, 0, len(klines))
	for _, kline := range klines {
		records = append(records, pendingClickHouseRecord{
			Kind:      pendingKindKline,
			Kline:     kline,
			Attempts:  1,
			LastError: errorString(writeErr),
			UpdatedAt: updatedAt,
		})
	}
	return w.enqueueMany(ctx, records)
}

func (w *ClickHousePendingWriter) Run(ctx context.Context) error {
	w.flushUntilIdleWithLogging(ctx)

	ticker := time.NewTicker(w.options.RetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.wake:
			if err := waitPendingFlushDebounce(ctx, pendingFlushDebounce); err != nil {
				return nil
			}
			w.flushUntilIdleWithLogging(ctx)
		case <-ticker.C:
			w.flushUntilIdleWithLogging(ctx)
		}
	}
}

func (w *ClickHousePendingWriter) Close() error {
	if w == nil || w.queue == nil {
		return nil
	}
	return w.queue.Close()
}

func waitPendingFlushDebounce(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (w *ClickHousePendingWriter) enqueueMany(ctx context.Context, records []pendingClickHouseRecord) error {
	if len(records) == 0 {
		return nil
	}
	if w == nil || w.queue == nil {
		return fmt.Errorf("clickhouse pending queue is nil")
	}
	payloads := make([][]byte, 0, len(records))
	for _, record := range records {
		payload, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal clickhouse pending record: %w", err)
		}
		payloads = append(payloads, payload)
	}
	if err := w.queue.Publish(ctx, payloads); err != nil {
		return fmt.Errorf("enqueue clickhouse pending record: %w", err)
	}
	w.signalFlush()
	return nil
}

func (w *ClickHousePendingWriter) signalFlush() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *ClickHousePendingWriter) flushWithLogging(ctx context.Context) {
	count, err := w.Flush(ctx)
	if err != nil && ctx.Err() == nil {
		slog.Error("flush clickhouse pending records failed", "count", count, "error", err)
		return
	}
	if count > 0 {
		slog.Info("flushed clickhouse pending records", "count", count)
	}
}

func (w *ClickHousePendingWriter) flushUntilIdleWithLogging(ctx context.Context) {
	count, err := w.FlushUntilIdle(ctx)
	if err != nil && ctx.Err() == nil {
		slog.Error("flush clickhouse pending records failed", "count", count, "error", err)
		return
	}
	if count > 0 {
		slog.Info("flushed clickhouse pending records", "count", count)
	}
}

func (w *ClickHousePendingWriter) FlushUntilIdle(ctx context.Context) (int, error) {
	total := 0
	for {
		count, err := w.Flush(ctx)
		total += count
		if err != nil {
			return total, err
		}
		if !shouldContinuePendingFlush(count, w.options.RetryBatch) {
			return total, nil
		}
		if ctx.Err() != nil {
			return total, nil
		}
	}
}

func shouldContinuePendingFlush(count int, retryBatch int) bool {
	if retryBatch <= 0 {
		retryBatch = 100
	}
	return count >= retryBatch
}

func (w *ClickHousePendingWriter) Flush(ctx context.Context) (int, error) {
	if w == nil || w.queue == nil {
		return 0, nil
	}
	messages, err := w.queue.Fetch(ctx, w.options.RetryBatch, w.options.RetryInterval)
	if err != nil {
		return 0, fmt.Errorf("claim clickhouse pending record: %w", err)
	}
	claims := make([]claimedPendingRecord, 0, len(messages))
	for _, message := range messages {
		payload := string(message.Payload)
		record, err := decodePendingRecord(payload)
		if err != nil {
			if deadErr := w.queue.DeadLetter(ctx, message, err.Error()); deadErr != nil {
				return 0, deadErr
			}
			if ackErr := w.queue.Ack(ctx, []pendingQueueMessage{message}); ackErr != nil {
				return 0, ackErr
			}
			slog.Error("drop invalid clickhouse pending record", "error", err)
			continue
		}
		claims = append(claims, claimedPendingRecord{
			message: message,
			payload: payload,
			record:  record,
		})
	}
	return w.writeClaimedRecords(ctx, claims)
}

func (w *ClickHousePendingWriter) writeRecord(ctx context.Context, record pendingClickHouseRecord) error {
	switch record.Kind {
	case pendingKindKline:
		return w.clickhouse.WriteKline(ctx, record.Kline)
	default:
		return fmt.Errorf("unsupported clickhouse pending kind %q", record.Kind)
	}
}

func (w *ClickHousePendingWriter) writeClaimedRecords(
	ctx context.Context,
	claims []claimedPendingRecord,
) (int, error) {
	if len(claims) == 0 {
		return 0, nil
	}

	batch, err := splitClaimedRecords(claims)
	if err != nil {
		return 0, err
	}

	flushed := 0
	if len(batch.ackOnlyClaims) > 0 {
		if err := w.ackClaims(ctx, batch.ackOnlyClaims); err != nil {
			return flushed, err
		}
		flushed += len(batch.ackOnlyClaims)
	}
	if len(batch.klineClaims) > 0 {
		if err := w.writeKlineClaims(ctx, batch.klineClaims); err != nil {
			return flushed, err
		}
		flushed += len(batch.klineClaims)
	}
	return flushed, nil
}

func splitClaimedRecords(claims []claimedPendingRecord) (pendingRecordBatch, error) {
	batch := pendingRecordBatch{
		klineClaims:   make([]claimedPendingRecord, 0, len(claims)),
		ackOnlyClaims: make([]claimedPendingRecord, 0),
	}
	for _, claim := range claims {
		switch claim.record.Kind {
		case pendingKindKline:
			batch.klineClaims = append(batch.klineClaims, claim)
		case "indicator":
			slog.Info("drop legacy clickhouse indicator pending record")
			batch.ackOnlyClaims = append(batch.ackOnlyClaims, claim)
		default:
			return pendingRecordBatch{}, fmt.Errorf("unsupported clickhouse pending kind %q", claim.record.Kind)
		}
	}
	return batch, nil
}

func (w *ClickHousePendingWriter) writeKlineClaims(ctx context.Context, claims []claimedPendingRecord) error {
	klines := make([]model.Kline, 0, len(claims))
	for _, claim := range claims {
		klines = append(klines, claim.record.Kline)
	}
	if err := w.clickhouse.WriteKlines(ctx, klines); err != nil {
		if deadErr := w.deadLetterMaxDelivered(ctx, claims, err); deadErr != nil {
			return deadErr
		}
		return err
	}
	return w.ackClaims(ctx, claims)
}

func (w *ClickHousePendingWriter) deadLetterMaxDelivered(
	ctx context.Context,
	claims []claimedPendingRecord,
	writeErr error,
) error {
	if w.options.MaxDeliveries <= 0 {
		return nil
	}
	for _, claim := range claims {
		if claim.message.DeliveryCount < int64(w.options.MaxDeliveries) {
			continue
		}
		if err := w.queue.DeadLetter(ctx, claim.message, errorString(writeErr)); err != nil {
			return err
		}
		if err := w.queue.Ack(ctx, []pendingQueueMessage{claim.message}); err != nil {
			return err
		}
	}
	return nil
}

func (w *ClickHousePendingWriter) ackClaims(ctx context.Context, claims []claimedPendingRecord) error {
	messages := make([]pendingQueueMessage, 0, len(claims))
	for _, claim := range claims {
		messages = append(messages, claim.message)
	}
	return w.queue.Ack(ctx, messages)
}

func decodePendingRecord(payload string) (pendingClickHouseRecord, error) {
	var record pendingClickHouseRecord
	if err := json.Unmarshal([]byte(payload), &record); err != nil {
		return pendingClickHouseRecord{}, fmt.Errorf("decode pending record: %w", err)
	}
	if record.Kind == "" {
		return pendingClickHouseRecord{}, fmt.Errorf("pending record kind cannot be empty")
	}
	if record.Kind != pendingKindKline && record.Kind != "indicator" {
		return pendingClickHouseRecord{}, fmt.Errorf("unsupported clickhouse pending kind %q", record.Kind)
	}
	return record, nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

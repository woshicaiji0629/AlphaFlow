package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/model"
	"github.com/redis/go-redis/v9"
)

const (
	clickHousePendingKey    = "market-data:clickhouse:pending"
	clickHouseProcessingKey = "market-data:clickhouse:processing"
	pendingKindKline        = "kline"
	pendingKindIndicator    = "indicator"
	pendingFlushDebounce    = 50 * time.Millisecond
)

type PendingWriterOptions struct {
	RetryInterval time.Duration
	RetryBatch    int
	MaxPending    int64
}

type ClickHousePendingWriter struct {
	client     *redis.Client
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
	payload string
	record  pendingClickHouseRecord
}

type pendingRecordBatch struct {
	klineClaims     []claimedPendingRecord
	indicatorClaims []claimedPendingRecord
}

func NewClickHousePendingWriter(
	client *redis.Client,
	clickhouse clickHouseWriter,
	options PendingWriterOptions,
) *ClickHousePendingWriter {
	if options.RetryInterval <= 0 {
		options.RetryInterval = 10 * time.Second
	}
	if options.RetryBatch <= 0 {
		options.RetryBatch = 100
	}
	if options.MaxPending <= 0 {
		options.MaxPending = 100000
	}
	return &ClickHousePendingWriter{
		client:     client,
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

func (w *ClickHousePendingWriter) EnqueueIndicator(
	ctx context.Context,
	snapshot model.IndicatorSnapshot,
	writeErr error,
) error {
	return w.enqueueMany(ctx, []pendingClickHouseRecord{{
		Kind:      pendingKindIndicator,
		Indicator: snapshot,
		Attempts:  1,
		LastError: errorString(writeErr),
		UpdatedAt: time.Now().UnixMilli(),
	}})
}

func (w *ClickHousePendingWriter) EnqueueIndicators(
	ctx context.Context,
	snapshots []model.IndicatorSnapshot,
	writeErr error,
) error {
	if len(snapshots) == 0 {
		return nil
	}
	updatedAt := time.Now().UnixMilli()
	records := make([]pendingClickHouseRecord, 0, len(snapshots))
	for _, snapshot := range snapshots {
		records = append(records, pendingClickHouseRecord{
			Kind:      pendingKindIndicator,
			Indicator: snapshot,
			Attempts:  1,
			LastError: errorString(writeErr),
			UpdatedAt: updatedAt,
		})
	}
	return w.enqueueMany(ctx, records)
}

func (w *ClickHousePendingWriter) Run(ctx context.Context) error {
	if err := w.recoverProcessing(ctx); err != nil {
		return err
	}
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
	payloads := make([]any, 0, len(records))
	for _, record := range records {
		payload, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal clickhouse pending record: %w", err)
		}
		payloads = append(payloads, payload)
	}
	pipe := w.client.TxPipeline()
	pipe.LPush(ctx, clickHousePendingKey, payloads...)
	pipe.LTrim(ctx, clickHousePendingKey, 0, w.options.MaxPending-1)
	if _, err := pipe.Exec(ctx); err != nil {
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

func (w *ClickHousePendingWriter) recoverProcessing(ctx context.Context) error {
	for {
		_, err := w.client.LMove(
			ctx,
			clickHouseProcessingKey,
			clickHousePendingKey,
			"RIGHT",
			"LEFT",
		).Result()
		if errors.Is(err, redis.Nil) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("recover clickhouse processing queue: %w", err)
		}
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
	claims := make([]claimedPendingRecord, 0, w.options.RetryBatch)
	for len(claims) < w.options.RetryBatch {
		payload, err := w.client.LMove(
			ctx,
			clickHousePendingKey,
			clickHouseProcessingKey,
			"RIGHT",
			"LEFT",
		).Result()
		if errors.Is(err, redis.Nil) {
			return w.writeClaimedRecords(ctx, claims)
		}
		if err != nil {
			return 0, fmt.Errorf("claim clickhouse pending record: %w", err)
		}

		record, err := decodePendingRecord(payload)
		if err != nil {
			if removeErr := w.removeProcessing(ctx, payload); removeErr != nil {
				return 0, removeErr
			}
			slog.Error("drop invalid clickhouse pending record", "error", err)
			continue
		}
		claims = append(claims, claimedPendingRecord{payload: payload, record: record})
	}
	return w.writeClaimedRecords(ctx, claims)
}

func (w *ClickHousePendingWriter) writeRecord(ctx context.Context, record pendingClickHouseRecord) error {
	switch record.Kind {
	case pendingKindKline:
		return w.clickhouse.WriteKline(ctx, record.Kline)
	case pendingKindIndicator:
		return w.clickhouse.WriteIndicator(ctx, record.Indicator)
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
	if len(batch.klineClaims) > 0 {
		if err := w.writeKlineClaims(ctx, batch.klineClaims); err != nil {
			return flushed, err
		}
		flushed += len(batch.klineClaims)
	}
	if len(batch.indicatorClaims) > 0 {
		if err := w.writeIndicatorClaims(ctx, batch.indicatorClaims); err != nil {
			return flushed, err
		}
		flushed += len(batch.indicatorClaims)
	}
	return flushed, nil
}

func splitClaimedRecords(claims []claimedPendingRecord) (pendingRecordBatch, error) {
	batch := pendingRecordBatch{
		klineClaims:     make([]claimedPendingRecord, 0, len(claims)),
		indicatorClaims: make([]claimedPendingRecord, 0, len(claims)),
	}
	for _, claim := range claims {
		switch claim.record.Kind {
		case pendingKindKline:
			batch.klineClaims = append(batch.klineClaims, claim)
		case pendingKindIndicator:
			batch.indicatorClaims = append(batch.indicatorClaims, claim)
		default:
			return pendingRecordBatch{}, fmt.Errorf("unsupported clickhouse pending kind %q", claim.record.Kind)
		}
	}
	return batch, nil
}

func (w *ClickHousePendingWriter) writeKlineClaims(ctx context.Context, claims []claimedPendingRecord) error {
	klines := make([]model.Kline, 0, len(claims))
	payloads := make([]string, 0, len(claims))
	for _, claim := range claims {
		klines = append(klines, claim.record.Kline)
		payloads = append(payloads, claim.payload)
	}
	if err := w.clickhouse.WriteKlines(ctx, klines); err != nil {
		if requeueErr := w.requeueClaims(ctx, claims, err); requeueErr != nil {
			return requeueErr
		}
		return err
	}
	return w.removeProcessingBatch(ctx, payloads)
}

func (w *ClickHousePendingWriter) writeIndicatorClaims(ctx context.Context, claims []claimedPendingRecord) error {
	indicators := make([]model.IndicatorSnapshot, 0, len(claims))
	payloads := make([]string, 0, len(claims))
	for _, claim := range claims {
		indicators = append(indicators, claim.record.Indicator)
		payloads = append(payloads, claim.payload)
	}
	if err := w.clickhouse.WriteIndicators(ctx, indicators); err != nil {
		if requeueErr := w.requeueClaims(ctx, claims, err); requeueErr != nil {
			return requeueErr
		}
		return err
	}
	return w.removeProcessingBatch(ctx, payloads)
}

func (w *ClickHousePendingWriter) requeueClaims(
	ctx context.Context,
	claims []claimedPendingRecord,
	writeErr error,
) error {
	for _, claim := range claims {
		if err := w.requeue(ctx, claim.payload, claim.record, writeErr); err != nil {
			return err
		}
	}
	return nil
}

func (w *ClickHousePendingWriter) requeue(
	ctx context.Context,
	payload string,
	record pendingClickHouseRecord,
	writeErr error,
) error {
	record.Attempts++
	record.LastError = errorString(writeErr)
	record.UpdatedAt = time.Now().UnixMilli()
	updated, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal clickhouse retry record: %w", err)
	}
	pipe := w.client.TxPipeline()
	pipe.LRem(ctx, clickHouseProcessingKey, 1, payload)
	pipe.LPush(ctx, clickHousePendingKey, updated)
	pipe.LTrim(ctx, clickHousePendingKey, 0, w.options.MaxPending-1)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("requeue clickhouse pending record: %w", err)
	}
	return nil
}

func (w *ClickHousePendingWriter) removeProcessing(ctx context.Context, payload string) error {
	if err := w.client.LRem(ctx, clickHouseProcessingKey, 1, payload).Err(); err != nil {
		return fmt.Errorf("remove clickhouse processing record: %w", err)
	}
	return nil
}

func (w *ClickHousePendingWriter) removeProcessingBatch(ctx context.Context, payloads []string) error {
	if len(payloads) == 0 {
		return nil
	}
	pipe := w.client.TxPipeline()
	for _, payload := range payloads {
		pipe.LRem(ctx, clickHouseProcessingKey, 1, payload)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("remove clickhouse processing records: %w", err)
	}
	return nil
}

func decodePendingRecord(payload string) (pendingClickHouseRecord, error) {
	var record pendingClickHouseRecord
	if err := json.Unmarshal([]byte(payload), &record); err != nil {
		return pendingClickHouseRecord{}, fmt.Errorf("decode pending record: %w", err)
	}
	if record.Kind == "" {
		return pendingClickHouseRecord{}, fmt.Errorf("pending record kind cannot be empty")
	}
	if record.Kind != pendingKindKline && record.Kind != pendingKindIndicator {
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

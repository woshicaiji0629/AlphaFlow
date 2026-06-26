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
)

type PendingWriterOptions struct {
	RetryInterval time.Duration
	RetryBatch    int
	MaxPending    int64
}

type ClickHousePendingWriter struct {
	client     *redis.Client
	clickhouse *ClickHouseStore
	options    PendingWriterOptions
}

type pendingClickHouseRecord struct {
	Kind      string                  `json:"kind"`
	Kline     model.Kline             `json:"kline,omitempty"`
	Indicator model.IndicatorSnapshot `json:"indicator,omitempty"`
	Attempts  int                     `json:"attempts"`
	LastError string                  `json:"last_error,omitempty"`
	UpdatedAt int64                   `json:"updated_at"`
}

func NewClickHousePendingWriter(
	client *redis.Client,
	clickhouse *ClickHouseStore,
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
	}
}

func (w *ClickHousePendingWriter) EnqueueKline(ctx context.Context, kline model.Kline, writeErr error) error {
	return w.enqueue(ctx, pendingClickHouseRecord{
		Kind:      pendingKindKline,
		Kline:     kline,
		Attempts:  1,
		LastError: errorString(writeErr),
		UpdatedAt: time.Now().UnixMilli(),
	})
}

func (w *ClickHousePendingWriter) EnqueueIndicator(
	ctx context.Context,
	snapshot model.IndicatorSnapshot,
	writeErr error,
) error {
	return w.enqueue(ctx, pendingClickHouseRecord{
		Kind:      pendingKindIndicator,
		Indicator: snapshot,
		Attempts:  1,
		LastError: errorString(writeErr),
		UpdatedAt: time.Now().UnixMilli(),
	})
}

func (w *ClickHousePendingWriter) Run(ctx context.Context) error {
	if err := w.recoverProcessing(ctx); err != nil {
		return err
	}
	w.flushWithLogging(ctx)

	ticker := time.NewTicker(w.options.RetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.flushWithLogging(ctx)
		}
	}
}

func (w *ClickHousePendingWriter) enqueue(ctx context.Context, record pendingClickHouseRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal clickhouse pending record: %w", err)
	}
	pipe := w.client.TxPipeline()
	pipe.LPush(ctx, clickHousePendingKey, payload)
	pipe.LTrim(ctx, clickHousePendingKey, 0, w.options.MaxPending-1)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("enqueue clickhouse pending record: %w", err)
	}
	return nil
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

func (w *ClickHousePendingWriter) Flush(ctx context.Context) (int, error) {
	var flushed int
	for flushed < w.options.RetryBatch {
		payload, err := w.client.LMove(
			ctx,
			clickHousePendingKey,
			clickHouseProcessingKey,
			"RIGHT",
			"LEFT",
		).Result()
		if errors.Is(err, redis.Nil) {
			return flushed, nil
		}
		if err != nil {
			return flushed, fmt.Errorf("claim clickhouse pending record: %w", err)
		}

		record, err := decodePendingRecord(payload)
		if err != nil {
			if removeErr := w.removeProcessing(ctx, payload); removeErr != nil {
				return flushed, removeErr
			}
			slog.Error("drop invalid clickhouse pending record", "error", err)
			continue
		}
		if err := w.writeRecord(ctx, record); err != nil {
			if requeueErr := w.requeue(ctx, payload, record, err); requeueErr != nil {
				return flushed, requeueErr
			}
			return flushed, err
		}
		if err := w.removeProcessing(ctx, payload); err != nil {
			return flushed, err
		}
		flushed++
	}
	return flushed, nil
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

func decodePendingRecord(payload string) (pendingClickHouseRecord, error) {
	var record pendingClickHouseRecord
	if err := json.Unmarshal([]byte(payload), &record); err != nil {
		return pendingClickHouseRecord{}, fmt.Errorf("decode pending record: %w", err)
	}
	if record.Kind == "" {
		return pendingClickHouseRecord{}, fmt.Errorf("pending record kind cannot be empty")
	}
	return record, nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type clickHouseBatch struct {
	klines []model.Kline
}

func (s *MarketStore) enqueueClickHouseKline(kline model.Kline) {
	s.clickHouseMu.Lock()
	s.pendingKlines = append(s.pendingKlines, kline)
	ready := len(s.pendingKlines) >= clickHouseFlushBatch
	s.clickHouseMu.Unlock()
	if ready {
		s.signalClickHouseFlush()
	}
}

func (s *MarketStore) signalClickHouseFlush() {
	select {
	case s.clickHouseFlushReady <- struct{}{}:
	default:
	}
}

func (s *MarketStore) runClickHouseFlush(ctx context.Context) error {
	ticker := time.NewTicker(clickHouseFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), clickHouseFlushTimeout)
			if err := s.flushAllClickHouse(flushCtx); err != nil {
				slog.Error("flush clickhouse batch failed during shutdown", "error", err)
			}
			cancel()
			return nil
		case <-ticker.C:
			if err := s.flushAllClickHouse(ctx); err != nil {
				slog.Error("flush clickhouse batch failed", "error", err)
			}
		case <-s.clickHouseFlushReady:
			if err := s.flushAllClickHouse(ctx); err != nil {
				slog.Error("flush clickhouse batch failed", "error", err)
			}
		}
	}
}

func (s *MarketStore) flushAllClickHouse(ctx context.Context) error {
	for {
		batch := s.drainClickHouse(clickHouseFlushBatch)
		if len(batch.klines) == 0 {
			return nil
		}
		if err := s.writeClickHouseBatch(ctx, batch); err != nil {
			return err
		}
	}
}

func (s *MarketStore) writeClickHouseBatch(ctx context.Context, batch clickHouseBatch) error {
	if len(batch.klines) > 0 {
		if err := s.clickhouse.WriteKlines(ctx, batch.klines); err != nil {
			if enqueueErr := s.enqueuePendingKlines(ctx, batch.klines, err); enqueueErr != nil {
				s.requeueClickHouse(batch)
				return enqueueErr
			}
			slog.Error("write kline batch to clickhouse failed, enqueue retry",
				"count", len(batch.klines),
				"error", err,
			)
		}
	}
	return nil
}

func (s *MarketStore) enqueuePendingKlines(ctx context.Context, klines []model.Kline, writeErr error) error {
	if s.pending == nil {
		return writeErr
	}
	if err := s.pending.EnqueueKlines(ctx, klines, writeErr); err != nil {
		return fmt.Errorf("enqueue clickhouse kline retry after batch write failure %w: %v", err, writeErr)
	}
	return nil
}

func (s *MarketStore) drainClickHouse(limit int) clickHouseBatch {
	s.clickHouseMu.Lock()
	defer s.clickHouseMu.Unlock()

	if limit <= 0 {
		limit = clickHouseFlushBatch
	}
	batch := clickHouseBatch{}
	if len(s.pendingKlines) > 0 {
		count := min(len(s.pendingKlines), limit)
		batch.klines = append(batch.klines, s.pendingKlines[:count]...)
		s.pendingKlines = append(s.pendingKlines[:0], s.pendingKlines[count:]...)
		limit -= count
	}
	return batch
}

func (s *MarketStore) requeueClickHouse(batch clickHouseBatch) {
	s.clickHouseMu.Lock()
	defer s.clickHouseMu.Unlock()

	if len(batch.klines) > 0 {
		s.pendingKlines = append(batch.klines, s.pendingKlines...)
	}
}

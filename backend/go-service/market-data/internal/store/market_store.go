package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type MarketStore struct {
	redis      *RedisStore
	clickhouse *ClickHouseStore
	pending    *ClickHousePendingWriter
}

type MarketStoreOptions struct {
	RetryInterval time.Duration
	RetryBatch    int
	MaxPending    int64
}

func NewMarketStore(redisStore *RedisStore, clickHouseStore *ClickHouseStore, options MarketStoreOptions) *MarketStore {
	marketStore := &MarketStore{
		redis:      redisStore,
		clickhouse: clickHouseStore,
	}
	if clickHouseStore != nil {
		marketStore.pending = NewClickHousePendingWriter(redisStore.client, clickHouseStore, PendingWriterOptions{
			RetryInterval: options.RetryInterval,
			RetryBatch:    options.RetryBatch,
			MaxPending:    options.MaxPending,
		})
	}
	return marketStore
}

func (s *MarketStore) LastOpenTime(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
) (int64, bool, error) {
	return s.redis.LastOpenTime(ctx, exchange, market, symbol, interval)
}

func (s *MarketStore) RangeKlines(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	end int64,
) ([]model.Kline, error) {
	return s.redis.RangeKlines(ctx, exchange, market, symbol, interval, start, end)
}

func (s *MarketStore) UpsertKline(ctx context.Context, kline model.Kline) error {
	if err := s.redis.UpsertKline(ctx, kline); err != nil {
		return err
	}
	if s.clickhouse == nil || !kline.IsClosed {
		return nil
	}
	if err := s.clickhouse.WriteKline(ctx, kline); err != nil {
		slog.Error("write kline to clickhouse failed, enqueue retry",
			"exchange", kline.Exchange,
			"market", kline.Market,
			"symbol", kline.Symbol,
			"interval", kline.Interval,
			"open_time", kline.OpenTime,
			"error", err,
		)
		if enqueueErr := s.pending.EnqueueKline(ctx, kline, err); enqueueErr != nil {
			return fmt.Errorf("enqueue clickhouse kline retry after write failure %w: %v", enqueueErr, err)
		}
	}
	return nil
}

func (s *MarketStore) SetLastPrice(ctx context.Context, price model.LastPrice) error {
	return s.redis.SetLastPrice(ctx, price)
}

func (s *MarketStore) SetMarkPrice(ctx context.Context, price model.MarkPrice) error {
	return s.redis.SetMarkPrice(ctx, price)
}

func (s *MarketStore) SetBookTicker(ctx context.Context, ticker model.BookTicker) error {
	return s.redis.SetBookTicker(ctx, ticker)
}

func (s *MarketStore) SetOpenInterest(ctx context.Context, interest model.OpenInterest) error {
	return s.redis.SetOpenInterest(ctx, interest)
}

func (s *MarketStore) AddLiquidation(ctx context.Context, liquidation model.Liquidation, limit int64) error {
	return s.redis.AddLiquidation(ctx, liquidation, limit)
}

func (s *MarketStore) SetMarketStatus(ctx context.Context, status model.MarketStatus) error {
	return s.redis.SetMarketStatus(ctx, status)
}

func (s *MarketStore) SetWebSocketStatus(ctx context.Context, status model.WebSocketStatus) error {
	return s.redis.SetWebSocketStatus(ctx, status)
}

func (s *MarketStore) IsMarketAvailable(ctx context.Context, exchange string, market string) (bool, error) {
	return s.redis.IsMarketAvailable(ctx, exchange, market)
}

func (s *MarketStore) SetIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	if err := s.redis.SetIndicator(ctx, snapshot); err != nil {
		return err
	}
	if s.clickhouse == nil {
		return nil
	}
	if err := s.clickhouse.WriteIndicator(ctx, snapshot); err != nil {
		slog.Error("write indicator to clickhouse failed, enqueue retry",
			"exchange", snapshot.Exchange,
			"market", snapshot.Market,
			"symbol", snapshot.Symbol,
			"interval", snapshot.Interval,
			"open_time", snapshot.OpenTime,
			"error", err,
		)
		if enqueueErr := s.pending.EnqueueIndicator(ctx, snapshot, err); enqueueErr != nil {
			return fmt.Errorf("enqueue clickhouse indicator retry after write failure %w: %v", enqueueErr, err)
		}
	}
	if err := s.redis.MarkIndicatorOpenTime(ctx, snapshot); err != nil {
		return err
	}
	return nil
}

func (s *MarketStore) LastIndicatorOpenTime(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
) (int64, bool, error) {
	return s.redis.LastIndicatorOpenTime(ctx, exchange, market, symbol, interval)
}

func (s *MarketStore) RunClickHouseRetry(ctx context.Context) error {
	if s == nil || s.pending == nil {
		<-ctx.Done()
		return nil
	}
	return s.pending.Run(ctx)
}

func (s *MarketStore) Close() error {
	if s == nil {
		return nil
	}
	return s.clickhouse.Close()
}

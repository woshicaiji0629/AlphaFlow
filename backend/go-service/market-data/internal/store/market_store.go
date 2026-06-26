package store

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

const (
	latestFlushInterval = 250 * time.Millisecond
	latestFlushTimeout  = 2 * time.Second
)

type MarketStore struct {
	redis       *RedisStore
	clickhouse  *ClickHouseStore
	pending     *ClickHousePendingWriter
	latestMu    sync.Mutex
	lastPrices  map[string]model.LastPrice
	markPrices  map[string]model.MarkPrice
	bookTickers map[string]model.BookTicker
}

type MarketStoreOptions struct {
	RetryInterval time.Duration
	RetryBatch    int
	MaxPending    int64
}

func NewMarketStore(redisStore *RedisStore, clickHouseStore *ClickHouseStore, options MarketStoreOptions) *MarketStore {
	marketStore := &MarketStore{
		redis:       redisStore,
		clickhouse:  clickHouseStore,
		lastPrices:  map[string]model.LastPrice{},
		markPrices:  map[string]model.MarkPrice{},
		bookTickers: map[string]model.BookTicker{},
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
	s.latestMu.Lock()
	s.lastPrices[model.LastPriceKey(price.Exchange, price.Market, price.Symbol)] = price
	s.latestMu.Unlock()
	return nil
}

func (s *MarketStore) SetMarkPrice(ctx context.Context, price model.MarkPrice) error {
	s.latestMu.Lock()
	s.markPrices[model.MarkPriceKey(price.Exchange, price.Market, price.Symbol)] = price
	s.latestMu.Unlock()
	return nil
}

func (s *MarketStore) SetBookTicker(ctx context.Context, ticker model.BookTicker) error {
	s.latestMu.Lock()
	s.bookTickers[model.BookTickerKey(ticker.Exchange, ticker.Market, ticker.Symbol)] = ticker
	s.latestMu.Unlock()
	return nil
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
	if s == nil {
		<-ctx.Done()
		return nil
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- s.runLatestFlush(ctx)
	}()
	go func() {
		if s.pending == nil {
			<-ctx.Done()
			errCh <- nil
			return
		}
		errCh <- s.pending.Run(ctx)
	}()

	err := <-errCh
	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func (s *MarketStore) Close() error {
	if s == nil {
		return nil
	}
	return s.clickhouse.Close()
}

type latestBatch struct {
	lastPrices  []model.LastPrice
	markPrices  []model.MarkPrice
	bookTickers []model.BookTicker
}

func (s *MarketStore) runLatestFlush(ctx context.Context) error {
	ticker := time.NewTicker(latestFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), latestFlushTimeout)
			if err := s.flushLatest(flushCtx); err != nil {
				slog.Error("flush latest market data failed during shutdown", "error", err)
			}
			cancel()
			return nil
		case <-ticker.C:
			if err := s.flushLatest(ctx); err != nil {
				slog.Error("flush latest market data failed", "error", err)
			}
		}
	}
}

func (s *MarketStore) flushLatest(ctx context.Context) error {
	batch := s.drainLatest()
	if len(batch.lastPrices) == 0 && len(batch.markPrices) == 0 && len(batch.bookTickers) == 0 {
		return nil
	}
	if err := s.redis.SetLatestBatch(ctx, batch.lastPrices, batch.markPrices, batch.bookTickers); err != nil {
		s.requeueLatest(batch)
		return err
	}
	return nil
}

func (s *MarketStore) drainLatest() latestBatch {
	s.latestMu.Lock()
	defer s.latestMu.Unlock()

	batch := latestBatch{
		lastPrices:  make([]model.LastPrice, 0, len(s.lastPrices)),
		markPrices:  make([]model.MarkPrice, 0, len(s.markPrices)),
		bookTickers: make([]model.BookTicker, 0, len(s.bookTickers)),
	}
	for _, price := range s.lastPrices {
		batch.lastPrices = append(batch.lastPrices, price)
	}
	for _, price := range s.markPrices {
		batch.markPrices = append(batch.markPrices, price)
	}
	for _, ticker := range s.bookTickers {
		batch.bookTickers = append(batch.bookTickers, ticker)
	}

	clear(s.lastPrices)
	clear(s.markPrices)
	clear(s.bookTickers)
	return batch
}

func (s *MarketStore) requeueLatest(batch latestBatch) {
	s.latestMu.Lock()
	defer s.latestMu.Unlock()

	for _, price := range batch.lastPrices {
		key := model.LastPriceKey(price.Exchange, price.Market, price.Symbol)
		if _, ok := s.lastPrices[key]; !ok {
			s.lastPrices[key] = price
		}
	}
	for _, price := range batch.markPrices {
		key := model.MarkPriceKey(price.Exchange, price.Market, price.Symbol)
		if _, ok := s.markPrices[key]; !ok {
			s.markPrices[key] = price
		}
	}
	for _, ticker := range batch.bookTickers {
		key := model.BookTickerKey(ticker.Exchange, ticker.Market, ticker.Symbol)
		if _, ok := s.bookTickers[key]; !ok {
			s.bookTickers[key] = ticker
		}
	}
}

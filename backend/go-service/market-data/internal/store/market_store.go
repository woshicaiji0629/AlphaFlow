package store

import (
	"context"
	"sync"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

const (
	latestFlushInterval = 250 * time.Millisecond
	latestFlushTimeout  = 2 * time.Second

	clickHouseFlushInterval = 500 * time.Millisecond
	clickHouseFlushTimeout  = 5 * time.Second
	clickHouseFlushBatch    = 1000
)

type clickHouseWriter interface {
	WriteKline(ctx context.Context, kline model.Kline) error
	WriteKlines(ctx context.Context, klines []model.Kline) error
	Close() error
}

type KlineHandler func(ctx context.Context, kline model.Kline) error

type MarketStore struct {
	redis          *RedisStore
	clickhouse     clickHouseWriter
	pending        *ClickHousePendingWriter
	klineHandlers  []KlineHandler
	latestMu       sync.Mutex
	lastPrices     map[string]model.LastPrice
	markPrices     map[string]model.MarkPrice
	bookTickers    map[string]model.BookTicker
	openInterests  map[string]model.OpenInterest
	openKlines     map[string]model.Kline
	indicators     map[string]model.IndicatorSnapshot
	indicatorWins  map[string]model.IndicatorWindowSnapshot
	indicatorRTs   map[string]model.IndicatorRealtimeSnapshot
	lastOpenTimes  map[string]int64
	statusMu       sync.Mutex
	marketStatuses map[string]model.MarketStatus

	clickHouseMu         sync.Mutex
	pendingKlines        []model.Kline
	clickHouseFlushReady chan struct{}
}

type MarketStoreOptions struct {
	RetryInterval time.Duration
	RetryBatch    int
	MaxDeliveries int
	PendingQueue  pendingQueue
}

func NewMarketStore(redisStore *RedisStore, clickHouseStore *ClickHouseStore, options MarketStoreOptions) *MarketStore {
	marketStore := &MarketStore{
		redis:                redisStore,
		clickhouse:           clickHouseStore,
		lastPrices:           map[string]model.LastPrice{},
		markPrices:           map[string]model.MarkPrice{},
		bookTickers:          map[string]model.BookTicker{},
		openInterests:        map[string]model.OpenInterest{},
		openKlines:           map[string]model.Kline{},
		indicators:           map[string]model.IndicatorSnapshot{},
		indicatorWins:        map[string]model.IndicatorWindowSnapshot{},
		indicatorRTs:         map[string]model.IndicatorRealtimeSnapshot{},
		lastOpenTimes:        map[string]int64{},
		marketStatuses:       map[string]model.MarketStatus{},
		clickHouseFlushReady: make(chan struct{}, 1),
	}
	if clickHouseStore != nil && options.PendingQueue != nil {
		marketStore.pending = NewClickHousePendingWriter(options.PendingQueue, clickHouseStore, PendingWriterOptions{
			RetryInterval: options.RetryInterval,
			RetryBatch:    options.RetryBatch,
			MaxDeliveries: options.MaxDeliveries,
		})
	}
	return marketStore
}

func (s *MarketStore) RunClickHouseRetry(ctx context.Context) error {
	if s == nil {
		<-ctx.Done()
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 3)
	go func() {
		errCh <- s.runLatestFlush(ctx)
	}()
	go func() {
		errCh <- s.runClickHouseFlush(ctx)
	}()
	go func() {
		if s.pending == nil {
			<-ctx.Done()
			errCh <- nil
			return
		}
		errCh <- s.pending.Run(ctx)
	}()

	for completed := 0; completed < 3; completed++ {
		err := <-errCh
		if err != nil && ctx.Err() == nil {
			cancel()
			for completed++; completed < 3; completed++ {
				<-errCh
			}
			return err
		}
	}
	return nil
}

func (s *MarketStore) Close() error {
	if s == nil {
		return nil
	}
	if s.pending != nil {
		if err := s.pending.Close(); err != nil {
			return err
		}
	}
	if s.clickhouse == nil {
		return nil
	}
	return s.clickhouse.Close()
}

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

	clickHouseFlushInterval = 500 * time.Millisecond
	clickHouseFlushTimeout  = 5 * time.Second
	clickHouseFlushBatch    = 1000
)

type clickHouseWriter interface {
	WriteKline(ctx context.Context, kline model.Kline) error
	WriteKlines(ctx context.Context, klines []model.Kline) error
	WriteIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error
	WriteIndicators(ctx context.Context, snapshots []model.IndicatorSnapshot) error
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
	lastOpenTimes  map[string]int64
	statusMu       sync.Mutex
	marketStatuses map[string]model.MarketStatus

	clickHouseMu         sync.Mutex
	pendingKlines        []model.Kline
	pendingIndicators    []model.IndicatorSnapshot
	clickHouseFlushReady chan struct{}
}

type MarketStoreOptions struct {
	RetryInterval time.Duration
	RetryBatch    int
	MaxPending    int64
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
		lastOpenTimes:        map[string]int64{},
		marketStatuses:       map[string]model.MarketStatus{},
		clickHouseFlushReady: make(chan struct{}, 1),
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

func (s *MarketStore) AddKlineHandler(handler KlineHandler) {
	if handler == nil {
		return
	}
	s.latestMu.Lock()
	s.klineHandlers = append(s.klineHandlers, handler)
	s.latestMu.Unlock()
}

func (s *MarketStore) rememberLastOpenTime(key string, openTime int64) {
	s.latestMu.Lock()
	if current, ok := s.lastOpenTimes[key]; !ok || openTime > current {
		s.lastOpenTimes[key] = openTime
	}
	s.latestMu.Unlock()
}

func (s *MarketStore) rememberLastOpenTimes(klines []model.Kline) {
	s.latestMu.Lock()
	for _, kline := range klines {
		if !kline.IsClosed {
			continue
		}
		key := model.RedisKey(kline.Exchange, kline.Market, kline.Symbol, kline.Interval)
		if current, ok := s.lastOpenTimes[key]; !ok || kline.OpenTime > current {
			s.lastOpenTimes[key] = kline.OpenTime
		}
	}
	s.latestMu.Unlock()
}

func (s *MarketStore) LastOpenTime(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
) (int64, bool, error) {
	key := model.RedisKey(exchange, market, symbol, interval)
	s.latestMu.Lock()
	lastOpenTime, ok := s.lastOpenTimes[key]
	s.latestMu.Unlock()
	if ok {
		return lastOpenTime, true, nil
	}

	lastOpenTime, ok, err := s.redis.LastOpenTime(ctx, exchange, market, symbol, interval)
	if err != nil || !ok {
		return lastOpenTime, ok, err
	}
	s.rememberLastOpenTime(key, lastOpenTime)
	return lastOpenTime, true, nil
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
	return s.UpsertKlines(ctx, []model.Kline{kline})
}

func (s *MarketStore) UpsertKlines(ctx context.Context, klines []model.Kline) error {
	if len(klines) == 0 {
		return nil
	}
	closed := make([]model.Kline, 0, len(klines))
	for _, kline := range klines {
		if kline.IsClosed {
			closed = append(closed, kline)
			continue
		}
		s.latestMu.Lock()
		s.openKlines[klineLatestKey(kline)] = kline
		s.latestMu.Unlock()
		s.notifyKlineHandlers(ctx, kline)
	}
	if len(closed) == 0 {
		return nil
	}
	if err := s.redis.UpsertKlines(ctx, closed); err != nil {
		return err
	}
	s.rememberLastOpenTimes(closed)
	for _, kline := range latestClosedKlines(closed) {
		s.notifyKlineHandlers(ctx, kline)
	}
	if s.clickhouse == nil {
		return nil
	}
	for _, kline := range closed {
		s.enqueueClickHouseKline(kline)
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
	s.latestMu.Lock()
	s.openInterests[model.OpenInterestKey(interest.Exchange, interest.Market, interest.Symbol)] = interest
	s.latestMu.Unlock()
	return nil
}

func (s *MarketStore) AddLiquidation(ctx context.Context, liquidation model.Liquidation, limit int64) error {
	return s.redis.AddLiquidation(ctx, liquidation, limit)
}

func (s *MarketStore) SetMarketStatus(ctx context.Context, status model.MarketStatus) error {
	if !s.shouldWriteMarketStatus(status) {
		return nil
	}
	if err := s.redis.SetMarketStatus(ctx, status); err != nil {
		return err
	}
	s.rememberMarketStatus(status)
	return nil
}

func (s *MarketStore) SetWebSocketStatus(ctx context.Context, status model.WebSocketStatus) error {
	return s.redis.SetWebSocketStatus(ctx, status)
}

func (s *MarketStore) IsMarketAvailable(ctx context.Context, exchange string, market string) (bool, error) {
	return s.redis.IsMarketAvailable(ctx, exchange, market)
}

func (s *MarketStore) SetIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	if err := s.redis.SetIndicatorWithOpenTime(ctx, snapshot); err != nil {
		return err
	}
	if s.clickhouse == nil {
		return nil
	}
	s.enqueueClickHouseIndicator(snapshot)
	return nil
}

func (s *MarketStore) SetLatestIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	s.latestMu.Lock()
	s.indicators[model.IndicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)] = snapshot
	s.latestMu.Unlock()
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

func (s *MarketStore) SetDataHealth(ctx context.Context, health model.DataHealth) error {
	return s.redis.SetDataHealth(ctx, health)
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
	if s.clickhouse == nil {
		return nil
	}
	return s.clickhouse.Close()
}

type latestBatch struct {
	lastPrices    []model.LastPrice
	markPrices    []model.MarkPrice
	bookTickers   []model.BookTicker
	openInterests []model.OpenInterest
	openKlines    []model.Kline
	indicators    []model.IndicatorSnapshot
}

type clickHouseBatch struct {
	klines     []model.Kline
	indicators []model.IndicatorSnapshot
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
	if len(batch.lastPrices) == 0 &&
		len(batch.markPrices) == 0 &&
		len(batch.bookTickers) == 0 &&
		len(batch.openInterests) == 0 &&
		len(batch.openKlines) == 0 &&
		len(batch.indicators) == 0 {
		return nil
	}
	if err := s.redis.SetLatestBatch(
		ctx,
		batch.lastPrices,
		batch.markPrices,
		batch.bookTickers,
		batch.openInterests,
		batch.openKlines,
		batch.indicators,
	); err != nil {
		s.requeueLatest(batch)
		return err
	}
	return nil
}

func (s *MarketStore) drainLatest() latestBatch {
	s.latestMu.Lock()
	defer s.latestMu.Unlock()

	batch := latestBatch{
		lastPrices:    make([]model.LastPrice, 0, len(s.lastPrices)),
		markPrices:    make([]model.MarkPrice, 0, len(s.markPrices)),
		bookTickers:   make([]model.BookTicker, 0, len(s.bookTickers)),
		openInterests: make([]model.OpenInterest, 0, len(s.openInterests)),
		openKlines:    make([]model.Kline, 0, len(s.openKlines)),
		indicators:    make([]model.IndicatorSnapshot, 0, len(s.indicators)),
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
	for _, interest := range s.openInterests {
		batch.openInterests = append(batch.openInterests, interest)
	}
	for _, kline := range s.openKlines {
		batch.openKlines = append(batch.openKlines, kline)
	}
	for _, snapshot := range s.indicators {
		batch.indicators = append(batch.indicators, snapshot)
	}

	clear(s.lastPrices)
	clear(s.markPrices)
	clear(s.bookTickers)
	clear(s.openInterests)
	clear(s.openKlines)
	clear(s.indicators)
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
	for _, interest := range batch.openInterests {
		key := model.OpenInterestKey(interest.Exchange, interest.Market, interest.Symbol)
		if _, ok := s.openInterests[key]; !ok {
			s.openInterests[key] = interest
		}
	}
	for _, kline := range batch.openKlines {
		key := klineLatestKey(kline)
		if _, ok := s.openKlines[key]; !ok {
			s.openKlines[key] = kline
		}
	}
	for _, snapshot := range batch.indicators {
		key := model.IndicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
		if _, ok := s.indicators[key]; !ok {
			s.indicators[key] = snapshot
		}
	}
}

func klineLatestKey(kline model.Kline) string {
	return model.RedisKey(kline.Exchange, kline.Market, kline.Symbol, kline.Interval)
}

func (s *MarketStore) notifyKlineHandlers(ctx context.Context, kline model.Kline) {
	s.latestMu.Lock()
	handlers := append([]KlineHandler(nil), s.klineHandlers...)
	s.latestMu.Unlock()

	for _, handler := range handlers {
		if err := handler(ctx, kline); err != nil && ctx.Err() == nil {
			slog.Warn(
				"handle kline event failed",
				"exchange", kline.Exchange,
				"market", kline.Market,
				"symbol", kline.Symbol,
				"interval", kline.Interval,
				"open_time", kline.OpenTime,
				"is_closed", kline.IsClosed,
				"error", err,
			)
		}
	}
}

func latestClosedKlines(klines []model.Kline) []model.Kline {
	if len(klines) == 0 {
		return nil
	}
	latestByKey := make(map[string]model.Kline, len(klines))
	for _, kline := range klines {
		if !kline.IsClosed {
			continue
		}
		key := klineLatestKey(kline)
		latest, ok := latestByKey[key]
		if !ok || kline.OpenTime > latest.OpenTime {
			latestByKey[key] = kline
		}
	}
	latest := make([]model.Kline, 0, len(latestByKey))
	for _, kline := range latestByKey {
		latest = append(latest, kline)
	}
	return latest
}

func (s *MarketStore) shouldWriteMarketStatus(status model.MarketStatus) bool {
	key := model.MarketStatusKey(status.Exchange, status.Market)
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	previous, ok := s.marketStatuses[key]
	if ok &&
		previous.Available == status.Available &&
		previous.Reason == status.Reason {
		return false
	}
	return true
}

func (s *MarketStore) rememberMarketStatus(status model.MarketStatus) {
	key := model.MarketStatusKey(status.Exchange, status.Market)
	s.statusMu.Lock()
	s.marketStatuses[key] = status
	s.statusMu.Unlock()
}

func (s *MarketStore) enqueueClickHouseKline(kline model.Kline) {
	s.clickHouseMu.Lock()
	s.pendingKlines = append(s.pendingKlines, kline)
	ready := len(s.pendingKlines)+len(s.pendingIndicators) >= clickHouseFlushBatch
	s.clickHouseMu.Unlock()
	if ready {
		s.signalClickHouseFlush()
	}
}

func (s *MarketStore) enqueueClickHouseIndicator(snapshot model.IndicatorSnapshot) {
	s.clickHouseMu.Lock()
	s.pendingIndicators = append(s.pendingIndicators, snapshot)
	ready := len(s.pendingKlines)+len(s.pendingIndicators) >= clickHouseFlushBatch
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
		if len(batch.klines) == 0 && len(batch.indicators) == 0 {
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
	if len(batch.indicators) > 0 {
		if err := s.clickhouse.WriteIndicators(ctx, batch.indicators); err != nil {
			if enqueueErr := s.enqueuePendingIndicators(ctx, batch.indicators, err); enqueueErr != nil {
				s.requeueClickHouse(batch)
				return enqueueErr
			}
			slog.Error("write indicator batch to clickhouse failed, enqueue retry",
				"count", len(batch.indicators),
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

func (s *MarketStore) enqueuePendingIndicators(
	ctx context.Context,
	indicators []model.IndicatorSnapshot,
	writeErr error,
) error {
	if s.pending == nil {
		return writeErr
	}
	if err := s.pending.EnqueueIndicators(ctx, indicators, writeErr); err != nil {
		return fmt.Errorf("enqueue clickhouse indicator retry after batch write failure %w: %v", err, writeErr)
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
	if limit > 0 && len(s.pendingIndicators) > 0 {
		count := min(len(s.pendingIndicators), limit)
		batch.indicators = append(batch.indicators, s.pendingIndicators[:count]...)
		s.pendingIndicators = append(s.pendingIndicators[:0], s.pendingIndicators[count:]...)
	}
	return batch
}

func (s *MarketStore) requeueClickHouse(batch clickHouseBatch) {
	s.clickHouseMu.Lock()
	defer s.clickHouseMu.Unlock()

	if len(batch.indicators) > 0 {
		s.pendingIndicators = append(batch.indicators, s.pendingIndicators...)
	}
	if len(batch.klines) > 0 {
		s.pendingKlines = append(batch.klines, s.pendingKlines...)
	}
}

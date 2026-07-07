package store

import (
	"context"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type latestBatch struct {
	lastPrices    []model.LastPrice
	markPrices    []model.MarkPrice
	bookTickers   []model.BookTicker
	openInterests []model.OpenInterest
	openKlines    []model.Kline
	indicators    []model.IndicatorSnapshot
	indicatorWins []model.IndicatorWindowSnapshot
	indicatorRTs  []model.IndicatorRealtimeSnapshot
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
		len(batch.indicators) == 0 &&
		len(batch.indicatorWins) == 0 &&
		len(batch.indicatorRTs) == 0 {
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
		batch.indicatorWins,
		batch.indicatorRTs,
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
		indicatorWins: make([]model.IndicatorWindowSnapshot, 0, len(s.indicatorWins)),
		indicatorRTs:  make([]model.IndicatorRealtimeSnapshot, 0, len(s.indicatorRTs)),
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
	for _, snapshot := range s.indicatorWins {
		batch.indicatorWins = append(batch.indicatorWins, snapshot)
	}
	for _, snapshot := range s.indicatorRTs {
		batch.indicatorRTs = append(batch.indicatorRTs, snapshot)
	}

	clear(s.lastPrices)
	clear(s.markPrices)
	clear(s.bookTickers)
	clear(s.openInterests)
	clear(s.openKlines)
	clear(s.indicators)
	clear(s.indicatorWins)
	clear(s.indicatorRTs)
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
	for _, snapshot := range batch.indicatorWins {
		key := model.IndicatorWindowLatestKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
		if _, ok := s.indicatorWins[key]; !ok {
			s.indicatorWins[key] = snapshot
		}
	}
	for _, snapshot := range batch.indicatorRTs {
		key := model.IndicatorRealtimeKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
		if _, ok := s.indicatorRTs[key]; !ok {
			s.indicatorRTs[key] = snapshot
		}
	}
}

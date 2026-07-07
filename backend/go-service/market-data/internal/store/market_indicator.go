package store

import (
	"context"

	"alphaflow/go-service/market-data/internal/model"
)

func (s *MarketStore) SetIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	return s.redis.SetIndicatorWithOpenTime(ctx, snapshot)
}

func (s *MarketStore) SetClosedIndicator(
	ctx context.Context,
	snapshot model.IndicatorSnapshot,
	windowSnapshot model.IndicatorWindowSnapshot,
) error {
	return s.redis.SetClosedIndicator(ctx, snapshot, windowSnapshot)
}

func (s *MarketStore) SetLatestIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	s.latestMu.Lock()
	s.indicators[model.IndicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)] = snapshot
	s.latestMu.Unlock()
	return nil
}

func (s *MarketStore) SetIndicatorWindow(
	ctx context.Context,
	snapshot model.IndicatorWindowSnapshot,
) error {
	return s.redis.SetIndicatorWindowWithOpenTime(ctx, snapshot)
}

func (s *MarketStore) SetLatestIndicatorWindow(
	ctx context.Context,
	snapshot model.IndicatorWindowSnapshot,
) error {
	s.latestMu.Lock()
	s.indicatorWins[model.IndicatorWindowLatestKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)] = snapshot
	s.latestMu.Unlock()
	return nil
}

func (s *MarketStore) SetIndicatorRealtime(
	ctx context.Context,
	snapshot model.IndicatorRealtimeSnapshot,
) error {
	s.latestMu.Lock()
	s.indicatorRTs[model.IndicatorRealtimeKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)] = snapshot
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

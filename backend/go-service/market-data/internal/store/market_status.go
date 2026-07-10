package store

import (
	"context"

	"alphaflow/go-service/market-data/internal/model"
)

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

func (s *MarketStore) IsSymbolAvailable(ctx context.Context, exchange string, market string, symbol string) (bool, error) {
	return s.redis.IsSymbolAvailable(ctx, exchange, market, symbol)
}

func (s *MarketStore) SetDataHealth(ctx context.Context, health model.DataHealth) error {
	return s.redis.SetDataHealth(ctx, health)
}

func (s *MarketStore) shouldWriteMarketStatus(status model.MarketStatus) bool {
	key := model.MarketStatusKey(status.Exchange, status.Market, status.Symbol)
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
	key := model.MarketStatusKey(status.Exchange, status.Market, status.Symbol)
	s.statusMu.Lock()
	s.marketStatuses[key] = status
	s.statusMu.Unlock()
}

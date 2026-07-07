package store

import (
	"context"
	"log/slog"

	"alphaflow/go-service/market-data/internal/model"
)

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
	notifyClosed := latestClosedKlinesAfter(closed, s.currentLastOpenTimes(closed))
	s.rememberLastOpenTimes(closed)
	for _, kline := range notifyClosed {
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

func (s *MarketStore) currentLastOpenTimes(klines []model.Kline) map[string]int64 {
	s.latestMu.Lock()
	defer s.latestMu.Unlock()

	lastOpenTimes := make(map[string]int64, len(klines))
	for _, kline := range klines {
		if !kline.IsClosed {
			continue
		}
		key := klineLatestKey(kline)
		if lastOpenTime, ok := s.lastOpenTimes[key]; ok {
			lastOpenTimes[key] = lastOpenTime
		}
	}
	return lastOpenTimes
}

func latestClosedKlines(klines []model.Kline) []model.Kline {
	return latestClosedKlinesAfter(klines, nil)
}

func latestClosedKlinesAfter(klines []model.Kline, lastOpenTimes map[string]int64) []model.Kline {
	if len(klines) == 0 {
		return nil
	}
	latestByKey := make(map[string]model.Kline, len(klines))
	for _, kline := range klines {
		if !kline.IsClosed {
			continue
		}
		key := klineLatestKey(kline)
		if lastOpenTime, ok := lastOpenTimes[key]; ok && kline.OpenTime <= lastOpenTime {
			continue
		}
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

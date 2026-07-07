package store

import (
	"context"
	"encoding/json"
	"fmt"

	"alphaflow/go-service/market-data/internal/model"
)

func (s *RedisStore) SetLastPrice(ctx context.Context, price model.LastPrice) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.LastPriceKey(price.Exchange, price.Market, price.Symbol)
	payload, err := json.Marshal(price)
	if err != nil {
		return fmt.Errorf("marshal last price: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set last price: %w", err)
	}
	return nil
}

func (s *RedisStore) SetMarkPrice(ctx context.Context, price model.MarkPrice) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.MarkPriceKey(price.Exchange, price.Market, price.Symbol)
	payload, err := json.Marshal(price)
	if err != nil {
		return fmt.Errorf("marshal mark price: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set mark price: %w", err)
	}
	return nil
}

func (s *RedisStore) SetBookTicker(ctx context.Context, ticker model.BookTicker) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.BookTickerKey(ticker.Exchange, ticker.Market, ticker.Symbol)
	payload, err := json.Marshal(ticker)
	if err != nil {
		return fmt.Errorf("marshal book ticker: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set book ticker: %w", err)
	}
	return nil
}

func (s *RedisStore) SetOpenInterest(ctx context.Context, interest model.OpenInterest) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.OpenInterestKey(interest.Exchange, interest.Market, interest.Symbol)
	payload, err := json.Marshal(interest)
	if err != nil {
		return fmt.Errorf("marshal open interest: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.PollingTTL).Err(); err != nil {
		return fmt.Errorf("set open interest: %w", err)
	}
	return nil
}

func (s *RedisStore) SetLatestBatch(
	ctx context.Context,
	lastPrices []model.LastPrice,
	markPrices []model.MarkPrice,
	bookTickers []model.BookTicker,
	openInterests []model.OpenInterest,
	openKlines []model.Kline,
	indicators []model.IndicatorSnapshot,
	indicatorWins []model.IndicatorWindowSnapshot,
	indicatorRTs []model.IndicatorRealtimeSnapshot,
) error {
	if len(lastPrices) == 0 &&
		len(markPrices) == 0 &&
		len(bookTickers) == 0 &&
		len(openInterests) == 0 &&
		len(openKlines) == 0 &&
		len(indicators) == 0 &&
		len(indicatorWins) == 0 &&
		len(indicatorRTs) == 0 {
		return nil
	}
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	pipe := s.client.Pipeline()
	hasWrites := false
	for _, price := range lastPrices {
		payload, err := json.Marshal(price)
		if err != nil {
			return fmt.Errorf("marshal last price: %w", err)
		}
		key := model.LastPriceKey(price.Exchange, price.Market, price.Symbol)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
		hasWrites = true
	}
	for _, price := range markPrices {
		payload, err := json.Marshal(price)
		if err != nil {
			return fmt.Errorf("marshal mark price: %w", err)
		}
		key := model.MarkPriceKey(price.Exchange, price.Market, price.Symbol)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
		hasWrites = true
	}
	for _, ticker := range bookTickers {
		payload, err := json.Marshal(ticker)
		if err != nil {
			return fmt.Errorf("marshal book ticker: %w", err)
		}
		key := model.BookTickerKey(ticker.Exchange, ticker.Market, ticker.Symbol)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
		hasWrites = true
	}
	for _, interest := range openInterests {
		payload, err := json.Marshal(interest)
		if err != nil {
			return fmt.Errorf("marshal open interest: %w", err)
		}
		key := model.OpenInterestKey(interest.Exchange, interest.Market, interest.Symbol)
		pipe.Set(ctx, key, payload, s.retention.PollingTTL)
		hasWrites = true
	}
	groupedKlines, err := groupKlineHashUpdates(openKlines)
	if err != nil {
		return err
	}
	for key, update := range groupedKlines {
		dataKey := klineDataKey(key)
		indexKey := klineIndexKey(key)
		pipe.HSet(ctx, dataKey, update.hashValues...)
		pipe.ZAdd(ctx, indexKey, update.indexMembers...)
		hasWrites = true
		s.maintainKlineKey(key, func() {
			pipe.Eval(ctx, trimKlineHashScript, []string{indexKey, dataKey}, klineTrimStopRank(s.retention.KlineLimit), s.retention.KlineTTL.Milliseconds())
			hasWrites = true
		})
	}
	for _, snapshot := range indicators {
		payload, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("marshal indicator: %w", err)
		}
		key := model.IndicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
		s.maintainIndicatorKey(key, func() {
			pipe.Expire(ctx, key, s.retention.LatestTTL)
			hasWrites = true
		})
		hasWrites = true
	}
	for _, snapshot := range indicatorWins {
		fields, err := indicatorWindowHashFields(snapshot, s.retention.LatestTTL)
		if err != nil {
			return err
		}
		key := model.IndicatorWindowLatestKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
		pipe.HSet(ctx, key, fields...)
		s.maintainIndicatorKey(key, func() {
			pipe.Expire(ctx, key, s.retention.LatestTTL)
			hasWrites = true
		})
		hasWrites = true
	}
	for _, snapshot := range indicatorRTs {
		fields, err := indicatorRealtimeHashFields(snapshot, s.retention.LatestTTL)
		if err != nil {
			return err
		}
		key := model.IndicatorRealtimeKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
		pipe.HSet(ctx, key, fields...)
		s.maintainIndicatorKey(key, func() {
			pipe.Expire(ctx, key, s.retention.LatestTTL)
			hasWrites = true
		})
		hasWrites = true
	}
	if !hasWrites {
		return nil
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("set latest batch: %w", err)
	}
	return nil
}

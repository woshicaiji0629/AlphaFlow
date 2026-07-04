package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"alphaflow/go-service/pkg/marketkeys"
	"alphaflow/go-service/pkg/strategy"
	"github.com/redis/go-redis/v9"
)

type priceReader interface {
	ReadPrice(ctx context.Context, target strategy.Target) (strategy.PriceView, error)
}

type redisStringGetter interface {
	Get(ctx context.Context, key string) *redis.StringCmd
}

type redisPriceReader struct {
	client redisStringGetter
}

func newRedisPriceReader(client redisStringGetter) redisPriceReader {
	return redisPriceReader{client: client}
}

func (r redisPriceReader) ReadPrice(ctx context.Context, target strategy.Target) (strategy.PriceView, error) {
	if r.client == nil {
		return strategy.PriceView{}, fmt.Errorf("redis client is required")
	}
	lastPrice, err := r.readLastPrice(ctx, target)
	if err != nil {
		return strategy.PriceView{}, err
	}
	markPrice, err := r.readMarkPrice(ctx, target)
	if err != nil {
		return strategy.PriceView{}, err
	}
	return strategy.PriceView{
		LastPrice: lastPrice,
		MarkPrice: markPrice,
	}, nil
}

func (r redisPriceReader) readLastPrice(ctx context.Context, target strategy.Target) (string, error) {
	key := marketkeys.LastPriceKey(target.Exchange, target.Market, target.Symbol)
	payload, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read last price %s: %w", key, err)
	}
	var value struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return "", fmt.Errorf("decode last price %s: %w", key, err)
	}
	return value.Price, nil
}

func (r redisPriceReader) readMarkPrice(ctx context.Context, target strategy.Target) (string, error) {
	key := marketkeys.MarkPriceKey(target.Exchange, target.Market, target.Symbol)
	payload, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read mark price %s: %w", key, err)
	}
	var value struct {
		MarkPrice string `json:"mark_price"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return "", fmt.Errorf("decode mark price %s: %w", key, err)
	}
	return value.MarkPrice, nil
}

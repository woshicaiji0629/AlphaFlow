package app

import (
	"context"
	"fmt"
	"testing"

	"alphaflow/go-service/pkg/marketkeys"
	"alphaflow/go-service/pkg/strategy"
	"github.com/redis/go-redis/v9"
)

func TestRedisPriceReaderReadsLastAndMarkPrice(t *testing.T) {
	client := fakeRedisStringGetter{
		values: map[string]string{
			marketkeys.LastPriceKey("binance", "um", "ETHUSDT"): `{"price":"101.25"}`,
			marketkeys.MarkPriceKey("binance", "um", "ETHUSDT"): `{"mark_price":"101.2"}`,
		},
	}
	reader := newRedisPriceReader(client)
	target := strategy.Target{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
	}

	price, err := reader.ReadPrice(context.Background(), target)
	if err != nil {
		t.Fatalf("ReadPrice() error = %v", err)
	}
	if price.LastPrice != "101.25" {
		t.Fatalf("last price = %q, want 101.25", price.LastPrice)
	}
	if price.MarkPrice != "101.2" {
		t.Fatalf("mark price = %q, want 101.2", price.MarkPrice)
	}
}

func TestRedisPriceReaderAllowsMissingMarkPrice(t *testing.T) {
	client := fakeRedisStringGetter{
		values: map[string]string{
			marketkeys.LastPriceKey("binance", "um", "ETHUSDT"): `{"price":"101.25"}`,
		},
		missing: map[string]bool{
			marketkeys.MarkPriceKey("binance", "um", "ETHUSDT"): true,
		},
	}
	reader := newRedisPriceReader(client)
	target := strategy.Target{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
	}

	price, err := reader.ReadPrice(context.Background(), target)
	if err != nil {
		t.Fatalf("ReadPrice() error = %v", err)
	}
	if price.LastPrice != "101.25" {
		t.Fatalf("last price = %q, want 101.25", price.LastPrice)
	}
	if price.MarkPrice != "" {
		t.Fatalf("mark price = %q, want empty", price.MarkPrice)
	}
}

type fakeRedisStringGetter struct {
	values  map[string]string
	missing map[string]bool
	errors  map[string]error
}

func (g fakeRedisStringGetter) Get(ctx context.Context, key string) *redis.StringCmd {
	if err := ctx.Err(); err != nil {
		return redis.NewStringResult("", err)
	}
	if err := g.errors[key]; err != nil {
		return redis.NewStringResult("", err)
	}
	if g.missing[key] {
		return redis.NewStringResult("", redis.Nil)
	}
	value, ok := g.values[key]
	if !ok {
		return redis.NewStringResult("", fmt.Errorf("unexpected key %s", key))
	}
	return redis.NewStringResult(value, nil)
}

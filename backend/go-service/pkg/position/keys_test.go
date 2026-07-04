package position

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestRedisKeyBacktest(t *testing.T) {
	key, err := RedisKey(Key{
		Scope:        strategy.PositionScopeBacktest,
		RunID:        "run-1",
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "keltner",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, key, "st:pos:bt:run-1:binance:um:ETHUSDT:keltner")
}

func TestRedisKeyPaper(t *testing.T) {
	key, err := RedisKey(Key{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "SOLUSDT",
		StrategyName: "supertrend",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, key, "st:pos:paper:binance:um:SOLUSDT:supertrend")
}

func TestRedisKeyLive(t *testing.T) {
	key, err := RedisKey(Key{
		Scope:        strategy.PositionScopeLive,
		Account:      "main",
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		PositionSide: strategy.ExchangePositionSideLong,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, key, "st:pos:live:main:binance:um:ETHUSDT:long")
}

func TestBacktestTempKeysKey(t *testing.T) {
	key, err := BacktestTempKeysKey("run-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, key, "st:bt:run-1:keys")
}

func TestRedisKeyRequiresRunIDForBacktest(t *testing.T) {
	_, err := RedisKey(Key{
		Scope:        strategy.PositionScopeBacktest,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "keltner",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func assertEqual(t *testing.T, got string, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

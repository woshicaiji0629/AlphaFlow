package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/marketmodel"
)

func TestRunLoadsHistoricalKlines(t *testing.T) {
	originalBuilder := buildMarketStore
	t.Cleanup(func() { buildMarketStore = originalBuilder })
	store := &fakeMarketStore{
		klines: []marketmodel.Kline{{Symbol: "ETHUSDT", Interval: "3m", OpenTime: 1000}},
	}
	buildMarketStore = func(ctx context.Context, cfg config.Config) (marketStore, error) {
		return store, nil
	}
	path := writeConfig(t, `
[runtime]
run_id = "run-1"
strategy_set = "supertrend"

[data]
exchange = "binance"
market = "um"
symbols = ["ETHUSDT"]
interval = "3m"
start_time = "2026-01-01T00:00:00Z"
end_time = "2026-01-02T00:00:00Z"

[clickhouse]
enabled = true

[logging]
output = "stdout"
`)

	if err := Run(context.Background(), path); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if store.request.Symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", store.request.Symbol)
	}
	if store.request.Start == 0 || store.request.End == 0 {
		t.Fatalf("request time range not set: %#v", store.request)
	}
	if !store.closed {
		t.Fatal("store closed = false, want true")
	}
}

func TestRunRequiresClickHouse(t *testing.T) {
	path := writeConfig(t, `
[runtime]
run_id = "run-1"
strategy_set = "supertrend"

[data]
exchange = "binance"
market = "um"
symbols = ["ETHUSDT"]
interval = "3m"
start_time = "2026-01-01T00:00:00Z"
end_time = "2026-01-02T00:00:00Z"
`)

	if err := Run(context.Background(), path); err == nil {
		t.Fatal("Run() error = nil, want clickhouse required error")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

type fakeMarketStore struct {
	request reader.Request
	klines  []marketmodel.Kline
	closed  bool
}

func (s *fakeMarketStore) RangeKlines(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	end int64,
) ([]marketmodel.Kline, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.request = reader.Request{
		Exchange: exchange,
		Market:   market,
		Symbol:   symbol,
		Interval: interval,
		Start:    start,
		End:      end,
	}
	return s.klines, nil
}

func (s *fakeMarketStore) Close() error {
	s.closed = true
	return nil
}

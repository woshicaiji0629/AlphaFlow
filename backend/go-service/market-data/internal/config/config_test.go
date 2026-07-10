package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"alphaflow/go-service/pkg/constants"
)

func TestLoadConfigFromTOML(t *testing.T) {
	path := writeConfig(t, `
[binance]
enabled = true
websocket_connections = 11
symbols = ["ethusdt", "btcusdt"]

[gate]
enabled = true
websocket_connections = 12
symbols = ["eth_usdt"]

[bitget]
enabled = true
websocket_connections = 13
symbols = ["ethusdt"]

[bybit]
enabled = true
websocket_connections = 14
symbols = ["ethusdt"]

[nats]
url = "nats://example:4222"

[clickhouse]
enabled = true
addr = "localhost:9000"
database = "alphaflow"
retry_interval = "2s"
pending_ack_wait = "45s"
pending_max_deliveries = 7

[backfill_queue]
ack_wait = "10m"
max_deliveries = 4
max_pending = 500
worker_enabled = true
worker_batch = 2
worker_max_wait = "3s"

[logging]
service = "test-service"
level = "debug"
format = "text"
output = "file"
dir = "logs"
filename = "test.log"
max_size_mb = 10
max_backups = 3
max_age_days = 7
compress = true

`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Binance.Symbols[0]; got != "ETHUSDT" {
		t.Fatalf("Binance symbol = %q, want ETHUSDT", got)
	}
	if got := cfg.Binance.WebSocketConnections; got != 11 {
		t.Fatalf("Binance websocket connections = %d, want 11", got)
	}
	if got := cfg.Gate.Symbols[0]; got != "ETH_USDT" {
		t.Fatalf("Gate symbol = %q, want ETH_USDT", got)
	}
	if got := cfg.Gate.WebSocketConnections; got != 12 {
		t.Fatalf("Gate websocket connections = %d, want 12", got)
	}
	if !cfg.Bitget.Enabled {
		t.Fatal("Bitget should be enabled")
	}
	if got := cfg.Bitget.Symbols[0]; got != "ETHUSDT" {
		t.Fatalf("Bitget symbol = %q, want ETHUSDT", got)
	}
	if got := cfg.Bitget.WebSocketConnections; got != 13 {
		t.Fatalf("Bitget websocket connections = %d, want 13", got)
	}
	if !cfg.Bybit.Enabled {
		t.Fatal("Bybit should be enabled")
	}
	if got := cfg.Bybit.Symbols[0]; got != "ETHUSDT" {
		t.Fatalf("Bybit symbol = %q, want ETHUSDT", got)
	}
	if got := cfg.Bybit.WebSocketConnections; got != 14 {
		t.Fatalf("Bybit websocket connections = %d, want 14", got)
	}
	if got := cfg.Logging.Level; got != "debug" {
		t.Fatalf("log level = %q, want debug", got)
	}
	if got := cfg.Logging.Format; got != "text" {
		t.Fatalf("log format = %q, want text", got)
	}
	if got := cfg.Logging.Output; got != "file" {
		t.Fatalf("log output = %q, want file", got)
	}
	if got := cfg.Logging.Service; got != "test-service" {
		t.Fatalf("log service = %q, want test-service", got)
	}
	if got := cfg.Logging.Dir; got != "logs" {
		t.Fatalf("log dir = %q, want logs", got)
	}
	if got := cfg.Logging.Filename; got != "test.log" {
		t.Fatalf("log filename = %q, want test.log", got)
	}
	if got := cfg.NATS.URL; got != "nats://example:4222" {
		t.Fatalf("nats url = %q, want nats://example:4222", got)
	}
	if got := cfg.ClickHouse.PendingMaxDeliveries; got != 7 {
		t.Fatalf("pending max deliveries = %d, want 7", got)
	}
	if wait, err := ClickHousePendingAckWait(cfg); err != nil {
		t.Fatalf("ClickHousePendingAckWait() error = %v", err)
	} else if wait != 45*time.Second {
		t.Fatalf("pending ack wait = %s, want 45s", wait)
	}
	if got := cfg.Backfill.MaxDeliveries; got != 4 {
		t.Fatalf("backfill max deliveries = %d, want 4", got)
	}
	if wait, err := BackfillAckWait(cfg); err != nil {
		t.Fatalf("BackfillAckWait() error = %v", err)
	} else if wait != 10*time.Minute {
		t.Fatalf("backfill ack wait = %s, want 10m", wait)
	}
	if !cfg.Backfill.WorkerEnabled {
		t.Fatal("backfill worker should be enabled")
	}
	if got := cfg.Backfill.WorkerBatch; got != 2 {
		t.Fatalf("backfill worker batch = %d, want 2", got)
	}
	if wait, err := BackfillWorkerMaxWait(cfg); err != nil {
		t.Fatalf("BackfillWorkerMaxWait() error = %v", err)
	} else if wait != 3*time.Second {
		t.Fatalf("backfill worker max wait = %s, want 3s", wait)
	}
}

func TestRejectsEnabledExchangeWithoutSymbols(t *testing.T) {
	path := writeConfig(t, `
[binance]
enabled = true
symbols = []
`)

	if _, err := Load(path); err == nil {
		t.Fatal("expected enabled exchange without symbols to be rejected")
	}
}

func TestRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, `
[binance]
enabled = true
symbols = ["ethusdt"]
unknown = true
`)

	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
}

func TestDefaultMarketPolicy(t *testing.T) {
	if RESTLimit() != 200 {
		t.Fatalf("RESTLimit = %d, want 200", RESTLimit())
	}
	if OpenInterestInterval() != time.Minute {
		t.Fatalf("OpenInterestInterval = %s, want 1m", OpenInterestInterval())
	}
	if MarkPriceInterval() != "1s" {
		t.Fatalf("MarkPriceInterval = %q, want 1s", MarkPriceInterval())
	}
	if IndicatorWarmupKlines() != 250 {
		t.Fatalf("IndicatorWarmupKlines = %d, want 250", IndicatorWarmupKlines())
	}
	if IndicatorWindowLookback() != 50 {
		t.Fatalf("IndicatorWindowLookback = %d, want 50", IndicatorWindowLookback())
	}
	if IndicatorCacheBuffer() != 10 {
		t.Fatalf("IndicatorCacheBuffer = %d, want 10", IndicatorCacheBuffer())
	}
	if IndicatorKlineCacheLimit() != 310 {
		t.Fatalf("IndicatorKlineCacheLimit = %d, want 310", IndicatorKlineCacheLimit())
	}
	if IndicatorSnapshotCacheLimit() != 60 {
		t.Fatalf("IndicatorSnapshotCacheLimit = %d, want 60", IndicatorSnapshotCacheLimit())
	}
	if KlineLimit() != IndicatorKlineCacheLimit() {
		t.Fatalf("KlineLimit = %d, want %d", KlineLimit(), IndicatorKlineCacheLimit())
	}
	if KlineTTL() != 24*time.Hour {
		t.Fatalf("KlineTTL = %s, want 24h", KlineTTL())
	}
	if LiquidationLimit() != 200 {
		t.Fatalf("LiquidationLimit = %d, want 200", LiquidationLimit())
	}
	if LiquidationTTL() != 24*time.Hour {
		t.Fatalf("LiquidationTTL = %s, want 24h", LiquidationTTL())
	}
	if LatestTTL() != 24*time.Hour {
		t.Fatalf("LatestTTL = %s, want 24h", LatestTTL())
	}
	if PollingTTL() != 24*time.Hour {
		t.Fatalf("PollingTTL = %s, want 24h", PollingTTL())
	}
	if got := BinanceIntervals(); len(got) == 0 || got[0] != "1m" {
		t.Fatalf("BinanceIntervals = %#v, want first 1m", got)
	}
	if BinanceRESTBase() == "" || BinanceWSBase() == "" {
		t.Fatal("Binance endpoints should not be empty")
	}
	if got := GateIntervals(); len(got) == 0 || got[0] != "1m" {
		t.Fatalf("GateIntervals = %#v, want first 1m", got)
	}
	if GateRESTBase() == "" || GateWSBase() == "" || GateSettle() != "usdt" {
		t.Fatal("Gate constants should be set")
	}
	if got := BitgetIntervals(); len(got) == 0 || got[0] != "1m" {
		t.Fatalf("BitgetIntervals = %#v, want first 1m", got)
	}
	if BitgetRESTBase() == "" || BitgetWSBase() == "" || BitgetProductType() != "USDT-FUTURES" {
		t.Fatal("Bitget constants should be set")
	}
	if got := BybitIntervals(); len(got) == 0 || got[0] != "1m" {
		t.Fatalf("BybitIntervals = %#v, want first 1m", got)
	}
	if BybitRESTBase() == "" || BybitWSBase() == "" || BybitCategory() != "linear" {
		t.Fatal("Bybit constants should be set")
	}
}

func TestRedisConfigs(t *testing.T) {
	configs := RedisConfigs()
	defaultRedis, ok := configs[constants.RedisDefaultInstance]
	if !ok {
		t.Fatal("default redis config missing")
	}
	if defaultRedis.Addr != "127.0.0.1:6380" {
		t.Fatalf("Redis addr = %q, want 127.0.0.1:6380", defaultRedis.Addr)
	}
	if defaultRedis.PoolSize < 64 || defaultRedis.PoolSize > 128 {
		t.Fatalf("Redis pool size = %d, want 64..128", defaultRedis.PoolSize)
	}
	if defaultRedis.MinIdleConns != defaultRedis.PoolSize/2 {
		t.Fatalf("Redis min idle conns = %d, want pool size / 2", defaultRedis.MinIdleConns)
	}
}

func TestRedisConfigsFromEnv(t *testing.T) {
	t.Setenv("ALPHAFLOW_REDIS_ADDR", "redis:6380")

	configs := RedisConfigs()
	defaultRedis := configs[constants.RedisDefaultInstance]
	if defaultRedis.Addr != "redis:6380" {
		t.Fatalf("Redis addr = %q, want redis:6380", defaultRedis.Addr)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

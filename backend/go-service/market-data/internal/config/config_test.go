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
rest_base = "https://example.test"
ws_base = "wss://example.test"
symbols = ["ethusdt", "btcusdt"]

[okx]
enabled = true
rest_base = "https://okx.example.test"
ws_base = "wss://okx.example.test"
symbols = ["eth-usdt-swap"]

[gate]
enabled = true
rest_base = "https://gate.example.test"
ws_base = "wss://gate.example.test"
settle = "USDT"
symbols = ["eth_usdt"]

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

[websocket]
reconnect_delay = "3s"

[retention]
kline_limit = 500
liquidation_limit = 100
latest_ttl = "12h"
polling_ttl = "6h"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Binance.RESTBase != "https://example.test" {
		t.Fatalf("RESTBase = %q", cfg.Binance.RESTBase)
	}
	if got := cfg.Binance.Symbols[0]; got != "ETHUSDT" {
		t.Fatalf("Binance symbol = %q, want ETHUSDT", got)
	}
	if !cfg.OKX.Enabled {
		t.Fatal("OKX should be enabled")
	}
	if got := cfg.OKX.Symbols[0]; got != "ETH-USDT-SWAP" {
		t.Fatalf("OKX symbol = %q, want ETH-USDT-SWAP", got)
	}
	if got := cfg.Gate.Settle; got != "usdt" {
		t.Fatalf("Gate settle = %q, want usdt", got)
	}
	if got := cfg.Gate.Symbols[0]; got != "ETH_USDT" {
		t.Fatalf("Gate symbol = %q, want ETH_USDT", got)
	}
	if got := cfg.Retention.LiquidationLimit; got != 100 {
		t.Fatalf("liquidation limit = %d, want 100", got)
	}
	if got := cfg.Retention.KlineLimit; got != 500 {
		t.Fatalf("kline limit = %d, want 500", got)
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
	if got := cfg.WebSocket.ReconnectDelay; got != "3s" {
		t.Fatalf("reconnect delay = %q, want 3s", got)
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
	if got := BinanceIntervals(); len(got) == 0 || got[0] != "1m" {
		t.Fatalf("BinanceIntervals = %#v, want first 1m", got)
	}
	if got := GateIntervals(); len(got) == 0 || got[0] != "1m" {
		t.Fatalf("GateIntervals = %#v, want first 1m", got)
	}
}

func TestRedisConfigs(t *testing.T) {
	configs := RedisConfigs()
	defaultRedis, ok := configs[constants.RedisDefaultInstance]
	if !ok {
		t.Fatal("default redis config missing")
	}
	if defaultRedis.Addr != "localhost:6379" {
		t.Fatalf("Redis addr = %q, want localhost:6379", defaultRedis.Addr)
	}
	if defaultRedis.PoolSize != 20 {
		t.Fatalf("Redis pool size = %d, want 20", defaultRedis.PoolSize)
	}
	if defaultRedis.MinIdleConns != 5 {
		t.Fatalf("Redis min idle conns = %d, want 5", defaultRedis.MinIdleConns)
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

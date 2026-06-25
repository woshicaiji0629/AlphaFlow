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
symbols = ["ethusdt", "btcusdt"]

[gate]
enabled = true
symbols = ["eth_usdt"]

[bitget]
enabled = true
symbols = ["ethusdt"]

[bybit]
enabled = true
symbols = ["ethusdt"]

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

`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Binance.Symbols[0]; got != "ETHUSDT" {
		t.Fatalf("Binance symbol = %q, want ETHUSDT", got)
	}
	if got := cfg.Gate.Symbols[0]; got != "ETH_USDT" {
		t.Fatalf("Gate symbol = %q, want ETH_USDT", got)
	}
	if !cfg.Bitget.Enabled {
		t.Fatal("Bitget should be enabled")
	}
	if got := cfg.Bitget.Symbols[0]; got != "ETHUSDT" {
		t.Fatalf("Bitget symbol = %q, want ETHUSDT", got)
	}
	if !cfg.Bybit.Enabled {
		t.Fatal("Bybit should be enabled")
	}
	if got := cfg.Bybit.Symbols[0]; got != "ETHUSDT" {
		t.Fatalf("Bybit symbol = %q, want ETHUSDT", got)
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
	if KlineLimit() != 500 {
		t.Fatalf("KlineLimit = %d, want 500", KlineLimit())
	}
	if KlineTTL() != 7*24*time.Hour {
		t.Fatalf("KlineTTL = %s, want 168h", KlineTTL())
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
	if defaultRedis.Addr != "localhost:6380" {
		t.Fatalf("Redis addr = %q, want localhost:6380", defaultRedis.Addr)
	}
	if defaultRedis.PoolSize != 20 {
		t.Fatalf("Redis pool size = %d, want 20", defaultRedis.PoolSize)
	}
	if defaultRedis.MinIdleConns != 5 {
		t.Fatalf("Redis min idle conns = %d, want 5", defaultRedis.MinIdleConns)
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

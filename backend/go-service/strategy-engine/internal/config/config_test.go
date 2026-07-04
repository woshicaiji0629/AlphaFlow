package config

import (
	"os"
	"path/filepath"
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestLoadNormalizesAndValidatesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[runtime]
scan_interval = "2s"

[redis]
addr = "localhost:6380"

[position]
scope = "paper"
account = "demo"
backtest_ttl = "1h"

[sizing]
margin_quote = 50
leverage = 20

[fee]
fee_rate = 0.001
rebate_pct = 10

[clickhouse]
enabled = true
addr = "localhost:9000"
database = "alphaflow"
dial_timeout = "5s"
read_timeout = "30s"

[[targets]]
exchange = "Binance"
market = "UM"
symbol = "ethusdt"
interval = "3m"
confirm_intervals = ["5m", "10m"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Targets[0].Exchange != "binance" {
		t.Fatalf("exchange = %q, want binance", cfg.Targets[0].Exchange)
	}
	if cfg.Targets[0].Symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", cfg.Targets[0].Symbol)
	}
	if PositionScope(cfg) != strategy.PositionScopePaper {
		t.Fatalf("scope = %q, want paper", PositionScope(cfg))
	}
	if got := Targets(cfg)[0].Account; got != "demo" {
		t.Fatalf("account = %q, want demo", got)
	}
	if !cfg.ClickHouse.Enabled {
		t.Fatal("clickhouse enabled = false, want true")
	}
	if _, err := ClickHouseDialTimeout(cfg); err != nil {
		t.Fatalf("ClickHouseDialTimeout() error = %v", err)
	}
}

func TestLoadRejectsUnsupportedScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[position]
scope = "live"

[[targets]]
exchange = "binance"
market = "um"
symbol = "ETHUSDT"
interval = "3m"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unsupported scope error")
	}
}

func TestLoadRejectsInvalidClickHouseWhenEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[clickhouse]
enabled = true
database = ""

[[targets]]
exchange = "binance"
market = "um"
symbol = "ETHUSDT"
interval = "3m"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want clickhouse validation error")
	}
}

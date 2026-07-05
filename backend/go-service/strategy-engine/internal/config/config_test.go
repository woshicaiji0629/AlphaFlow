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

[nats]
url = "nats://localhost:4222"

[output]
mode = "bus"
stream = "ALPHAFLOW_STRATEGY"
subject = "strategy.decision"
default_ttl = "45s"

[position]
scope = "paper"
account = "demo"

[strategies]
enabled = [" SuperTrend "]

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
	if len(cfg.Strategies.Enabled) != 1 || cfg.Strategies.Enabled[0] != "supertrend" {
		t.Fatalf("strategies enabled = %#v, want [supertrend]", cfg.Strategies.Enabled)
	}
	if ttl, err := OutputDefaultTTL(cfg); err != nil {
		t.Fatalf("OutputDefaultTTL() error = %v", err)
	} else if ttl.String() != "45s" {
		t.Fatalf("output ttl = %s, want 45s", ttl)
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

func TestLoadRejectsEmptyStrategies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[strategies]
enabled = []

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
		t.Fatal("Load() error = nil, want strategies validation error")
	}
}

func TestLoadRejectsUnsupportedOutputMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[output]
mode = "direct"

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
		t.Fatal("Load() error = nil, want output mode validation error")
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

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[runtime]
scan_interval = "2s"
unknown = true

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
		t.Fatal("Load() error = nil, want unknown field error")
	}
}

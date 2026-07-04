package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadNormalizesAndValidatesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[runtime]
run_id = "run-1"
strategy_set = "supertrend"

[data]
exchange = "Binance"
market = "UM"
symbols = ["ethusdt"]
interval = "3m"
warmup_bars = 300
start_time = "2026-01-01T00:00:00Z"
end_time = "2026-01-02T00:00:00Z"

[sizing]
initial_equity = 10000
margin_quote = 100
leverage = 20

[fee]
fee_rate = 0.001
rebate_pct = 10
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Data.Exchange != "binance" {
		t.Fatalf("exchange = %q, want binance", cfg.Data.Exchange)
	}
	if cfg.Data.Symbols[0] != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", cfg.Data.Symbols[0])
	}
	if cfg.Data.WarmupBars != 300 {
		t.Fatalf("warmup bars = %d, want 300", cfg.Data.WarmupBars)
	}
	startTime, err := StartTime(cfg)
	if err != nil {
		t.Fatalf("StartTime() error = %v", err)
	}
	endTime, err := EndTime(cfg)
	if err != nil {
		t.Fatalf("EndTime() error = %v", err)
	}
	if !endTime.After(startTime) {
		t.Fatal("end time should be after start time")
	}
}

func TestLoadRejectsNegativeWarmupBars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[runtime]
run_id = "run-1"
strategy_set = "supertrend"

[data]
exchange = "binance"
market = "um"
symbols = ["ETHUSDT"]
interval = "3m"
warmup_bars = -1
start_time = "2026-01-01T00:00:00Z"
end_time = "2026-01-02T00:00:00Z"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want warmup_bars validation error")
	}
}

func TestLoadRejectsMissingRunID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[runtime]
run_id = ""
strategy_set = "supertrend"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want run_id validation error")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[runtime]
run_id = "run-1"
strategy_set = "supertrend"
unknown = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unknown field error")
	}
}

func TestLoadRejectsInvalidTimeRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[runtime]
run_id = "run-1"
strategy_set = "supertrend"

[data]
exchange = "binance"
market = "um"
symbols = ["ETHUSDT"]
interval = "3m"
start_time = "2026-01-02T00:00:00Z"
end_time = "2026-01-01T00:00:00Z"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want time range validation error")
	}
}

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

[execution]
slippage_bps = 2

[result]
event_batch_size = 123
trade_batch_size = 45
report_json_path = " reports/backtest.json "

[[symbol_specs]]
symbol = "ethusdt"
quantity_unit = "Base"
quantity_step = 0.001
min_quantity = 0.001
min_notional = 5
contract_size = 1
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
	if cfg.Result.EventBatchSize != 123 {
		t.Fatalf("event batch size = %d, want 123", cfg.Result.EventBatchSize)
	}
	if cfg.Result.TradeBatchSize != 45 {
		t.Fatalf("trade batch size = %d, want 45", cfg.Result.TradeBatchSize)
	}
	if cfg.Result.ReportJSONPath != "reports/backtest.json" {
		t.Fatalf("report json path = %q, want reports/backtest.json", cfg.Result.ReportJSONPath)
	}
	if cfg.Execution.SlippageBps != 2 {
		t.Fatalf("slippage bps = %f, want 2", cfg.Execution.SlippageBps)
	}
	if len(cfg.SymbolSpecs) != 1 {
		t.Fatalf("symbol specs len = %d, want 1", len(cfg.SymbolSpecs))
	}
	spec := cfg.SymbolSpecs[0]
	if spec.Exchange != "binance" || spec.Market != "um" || spec.Symbol != "ETHUSDT" || spec.QuantityUnit != "base" {
		t.Fatalf("symbol spec = %#v, want normalized exchange/market/symbol/unit", spec)
	}
}

func TestLoadRejectsInvalidResultBatchSize(t *testing.T) {
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
start_time = "2026-01-01T00:00:00Z"
end_time = "2026-01-02T00:00:00Z"

[result]
event_batch_size = 0
trade_batch_size = 100
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want result batch validation error")
	}
}

func TestLoadStrategySpec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[runtime]
run_id = "run-spec"

[strategy]
name = " SuperTrend "
enabled = true

[strategy.params]
entry_threshold = "0.80"

[data]
exchange = "binance"
market = "um"
symbols = ["ETHUSDT"]
interval = "3m"
start_time = "2026-01-01T00:00:00Z"
end_time = "2026-01-02T00:00:00Z"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	spec := StrategySpec(cfg)
	if spec.Name != "supertrend" {
		t.Fatalf("strategy spec = %#v", spec)
	}
	if spec.Params["entry_threshold"] != "0.80" {
		t.Fatalf("strategy params = %#v", spec.Params)
	}
}

func TestLoadRejectsNegativeSlippageBps(t *testing.T) {
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
start_time = "2026-01-01T00:00:00Z"
end_time = "2026-01-02T00:00:00Z"

[execution]
slippage_bps = -1
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want slippage validation error")
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

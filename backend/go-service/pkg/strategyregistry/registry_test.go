package strategyregistry

import (
	"context"
	"testing"

	"alphaflow/go-service/pkg/strategies/supertrend"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyspec"
)

func TestBuildSupertrend(t *testing.T) {
	item, err := Build(" supertrend ")
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if item.Name() != supertrend.Name {
		t.Fatalf("strategy name = %q, want %q", item.Name(), supertrend.Name)
	}
}

func TestSupportedMatchesBuildableStrategies(t *testing.T) {
	items := Supported()
	if len(items) != 1 || items[0].Code != "supertrend" {
		t.Fatalf("Supported()=%#v", items)
	}
	for _, name := range []string{
		"entry_profile",
		"entry_threshold",
		"max_blocking_timeframes",
		"intraday_min_aligned_timeframes",
		"min_take_profit_bps",
		"min_reward_risk_ratio",
		"max_stop_loss_bps",
		"exit_mode",
		"trailing_stop_pct",
		"profit_guard_activation_bps",
		"profit_guard_floor_bps",
		"profit_decay_activation_bps",
		"round_trip_cost_bps",
		"profit_buffer_bps",
		"micro_profit_quote",
		"target_profit_quote",
		"runner_profit_quote",
	} {
		if _, ok := items[0].Parameters[name]; !ok {
			t.Fatalf("Supported() missing parameter %q: %#v", name, items[0].Parameters)
		}
	}
	if !IsSupported(" SUPERTrend ") || IsSupported("unknown") {
		t.Fatal("IsSupported returned unexpected result")
	}
}

func TestBuildSet(t *testing.T) {
	strategies, err := BuildSet([]string{"supertrend"})
	if err != nil {
		t.Fatalf("BuildSet() error = %v", err)
	}
	if len(strategies) != 1 {
		t.Fatalf("strategies len = %d, want 1", len(strategies))
	}
	if strategies[0].Name() != supertrend.Name {
		t.Fatalf("strategy name = %q, want %q", strategies[0].Name(), supertrend.Name)
	}
}

func TestBuildRejectsUnsupportedStrategySet(t *testing.T) {
	_, err := Build("unknown")
	if err == nil {
		t.Fatal("Build() error = nil, want unsupported strategy error")
	}
}

func TestBuildSpecUsesConfiguredParameters(t *testing.T) {
	item, err := BuildSpec(strategyspec.Spec{
		Name:    "supertrend",
		Enabled: true,
		Params: map[string]string{
			"entry_threshold":         "0.80",
			"max_blocking_timeframes": "2",
			"min_take_profit_bps":     "26",
			"min_reward_risk_ratio":   "1.25",
			"max_stop_loss_bps":       "50",
		},
	})
	if err != nil {
		t.Fatalf("BuildSpec() error = %v", err)
	}
	if item.Name() != "supertrend" {
		t.Fatalf("strategy name = %q, want supertrend", item.Name())
	}
	result, err := item.Evaluate(context.Background(), strategy.Snapshot{
		Health: strategy.HealthView{OK: true},
	}, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.StrategyName != "supertrend" || result.Signal.Strategy != "supertrend" {
		t.Fatalf("result strategy identity = %q/%q", result.StrategyName, result.Signal.Strategy)
	}
}

func TestBuildSpecUsesTrailingExitMode(t *testing.T) {
	item, err := BuildSpec(strategyspec.Spec{
		Name:    "supertrend",
		Enabled: true,
		Params: map[string]string{
			"exit_mode":                   " Trailing ",
			"trailing_stop_pct":           "0.5",
			"profit_guard_activation_bps": "35",
			"profit_guard_floor_bps":      "18",
			"profit_decay_activation_bps": "60",
		},
	})
	if err != nil {
		t.Fatalf("BuildSpec() error = %v", err)
	}
	if item.Name() != supertrend.Name {
		t.Fatalf("strategy name = %q, want %q", item.Name(), supertrend.Name)
	}
}

func TestBuildSpecUsesIntradayAdaptiveProfile(t *testing.T) {
	item, err := BuildSpec(strategyspec.Spec{
		Name:    "supertrend",
		Enabled: true,
		Params: map[string]string{
			"entry_profile":                   "intraday_adaptive",
			"intraday_min_aligned_timeframes": "2",
			"exit_mode":                       "adaptive",
			"max_stop_loss_bps":               "70",
			"round_trip_cost_bps":             "16",
			"profit_buffer_bps":               "8",
			"micro_profit_quote":              "10",
			"target_profit_quote":             "20",
			"runner_profit_quote":             "30",
		},
	})
	if err != nil {
		t.Fatalf("BuildSpec() error = %v", err)
	}
	if item.Name() != supertrend.Name {
		t.Fatalf("strategy name = %q, want %q", item.Name(), supertrend.Name)
	}
}

func TestBuildSpecsRejectsDuplicateName(t *testing.T) {
	_, err := BuildSpecs([]strategyspec.Spec{
		{Name: "supertrend", Enabled: true},
		{Name: "supertrend", Enabled: true},
	})
	if err == nil {
		t.Fatal("BuildSpecs() error = nil, want duplicate id error")
	}
}

func TestBuildSpecRejectsUnknownParameter(t *testing.T) {
	_, err := BuildSpec(strategyspec.Spec{
		Name:    "supertrend",
		Enabled: true,
		Params:  map[string]string{"unknown": "value"},
	})
	if err == nil {
		t.Fatal("BuildSpec() error = nil, want unknown parameter error")
	}
}

func TestBuildSpecRejectsInvalidExitParameter(t *testing.T) {
	tests := map[string]map[string]string{
		"unknown entry profile": {"entry_profile": "scalper"},
		"zero intraday alignment": {
			"entry_profile":                   "intraday_adaptive",
			"intraday_min_aligned_timeframes": "0",
		},
		"too many intraday alignments": {
			"entry_profile":                   "intraday_adaptive",
			"intraday_min_aligned_timeframes": "5",
		},
		"intraday alignment without profile": {
			"intraday_min_aligned_timeframes": "2",
		},
		"negative take profit":      {"min_take_profit_bps": "-1"},
		"infinite take profit":      {"min_take_profit_bps": "+Inf"},
		"negative ratio":            {"min_reward_risk_ratio": "-0.1"},
		"nan ratio":                 {"min_reward_risk_ratio": "NaN"},
		"negative stop loss":        {"max_stop_loss_bps": "-1"},
		"infinite stop loss":        {"max_stop_loss_bps": "+Inf"},
		"stop loss at full price":   {"max_stop_loss_bps": "10000"},
		"unknown exit mode":         {"exit_mode": "partial"},
		"trailing distance missing": {"exit_mode": "trailing"},
		"negative trailing distance": {
			"exit_mode":         "trailing",
			"trailing_stop_pct": "-0.5",
		},
		"nan trailing distance": {
			"exit_mode":         "trailing",
			"trailing_stop_pct": "NaN",
		},
		"trailing distance without mode": {"trailing_stop_pct": "0.5"},
		"trailing with take profit distance": {
			"exit_mode":           "trailing",
			"trailing_stop_pct":   "0.5",
			"min_take_profit_bps": "26",
		},
		"trailing with reward risk ratio": {
			"exit_mode":             "trailing",
			"trailing_stop_pct":     "0.5",
			"min_reward_risk_ratio": "1.25",
		},
		"profit guard without trailing mode": {
			"profit_guard_activation_bps": "30",
		},
		"negative profit guard activation": {
			"exit_mode":                   "trailing",
			"trailing_stop_pct":           "0.5",
			"profit_guard_activation_bps": "-1",
		},
		"profit floor reaches activation": {
			"exit_mode":                   "trailing",
			"trailing_stop_pct":           "0.5",
			"profit_guard_activation_bps": "30",
			"profit_guard_floor_bps":      "30",
		},
		"profit decay before guard activation": {
			"exit_mode":                   "trailing",
			"trailing_stop_pct":           "0.5",
			"profit_guard_activation_bps": "60",
			"profit_decay_activation_bps": "50",
		},
		"adaptive quote profits out of order": {
			"exit_mode":           "adaptive",
			"micro_profit_quote":  "20",
			"target_profit_quote": "20",
			"runner_profit_quote": "30",
		},
		"adaptive with fixed trailing distance": {
			"exit_mode":         "adaptive",
			"trailing_stop_pct": "0.5",
		},
		"adaptive parameter without adaptive mode": {
			"micro_profit_quote": "10",
		},
		"negative adaptive cost": {
			"exit_mode":           "adaptive",
			"round_trip_cost_bps": "-1",
		},
	}
	for name, params := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := BuildSpec(strategyspec.Spec{
				Name:    "supertrend",
				Enabled: true,
				Params:  params,
			})
			if err == nil {
				t.Fatal("BuildSpec() error = nil, want invalid parameter error")
			}
		})
	}
}

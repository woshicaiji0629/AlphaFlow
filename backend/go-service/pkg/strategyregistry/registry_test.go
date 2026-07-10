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

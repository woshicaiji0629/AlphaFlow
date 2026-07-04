package strategyregistry

import (
	"testing"

	"alphaflow/go-service/pkg/strategies/supertrend"
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

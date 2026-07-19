package marketregime

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestV5FastUnlockRequiresAlignedExpansion(t *testing.T) {
	analyzer, err := NewV5Analyzer(DefaultV5Config())
	if err != nil {
		t.Fatal(err)
	}
	analyzer.gate.state = StateChopLock
	for index := 0; index < analyzer.config.BreakoutWindow; index++ {
		analyzer.appendBar(priceBar{high: 101, low: 99, close: 100})
	}
	base := Result{State: StateChopLock, DirectionScore: 60}
	evidence := v4PhaseEvidence{widthState: "expanding", momentum: 1, momentumDelta: 0.5}
	snapshot := v5Snapshot(1.3)
	state, direction, reasons := analyzer.target(snapshot, priceBar{close: 102}, base, evidence)
	if state != StateTrendArmed || direction != DirectionLong || reasons[len(reasons)-1] != "v5_fast_release_confirmed" {
		t.Fatalf("state=%q direction=%q reasons=%v", state, direction, reasons)
	}
}

func TestV5FastUnlockRejectsWeakEvidence(t *testing.T) {
	tests := []struct {
		name       string
		base       Result
		evidence   v4PhaseEvidence
		volume     float64
		wantReason string
	}{
		{name: "width", base: Result{State: StateChopLock, DirectionScore: 60}, evidence: v4PhaseEvidence{widthState: "stable", momentum: 1, momentumDelta: 0.5}, volume: 1.3, wantReason: "v5_breakout_width_weak"},
		{name: "direction", base: Result{State: StateChopLock, DirectionScore: 20}, evidence: v4PhaseEvidence{widthState: "expanding", momentum: 1, momentumDelta: 0.5}, volume: 1.3, wantReason: "v5_breakout_direction_weak"},
		{name: "momentum", base: Result{State: StateChopLock, DirectionScore: 60}, evidence: v4PhaseEvidence{widthState: "expanding", momentum: 1, momentumDelta: -0.5}, volume: 1.3, wantReason: "v5_breakout_momentum_weak"},
		{name: "volume", base: Result{State: StateChopLock, DirectionScore: 60}, evidence: v4PhaseEvidence{widthState: "expanding", momentum: 1, momentumDelta: 0.5}, volume: 1.1, wantReason: "v5_breakout_volume_weak"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analyzer, err := NewV5Analyzer(DefaultV5Config())
			if err != nil {
				t.Fatal(err)
			}
			analyzer.gate.state = StateChopLock
			for index := 0; index < analyzer.config.BreakoutWindow; index++ {
				analyzer.appendBar(priceBar{high: 101, low: 99, close: 100})
			}
			state, direction, reasons := analyzer.target(v5Snapshot(test.volume), priceBar{close: 102}, test.base, test.evidence)
			if state != StateChopLock || direction != DirectionNone || reasons[len(reasons)-1] != test.wantReason {
				t.Fatalf("state=%q direction=%q reasons=%v", state, direction, reasons)
			}
		})
	}
}

func TestVersionedAnalyzerConstructsV5(t *testing.T) {
	analyzer, err := NewAnalyzer(VersionV5, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if analyzer.Version() != VersionV5 {
		t.Fatalf("version=%q", analyzer.Version())
	}
}

func v5Snapshot(volumeRatio float64) strategy.Snapshot {
	return strategy.Snapshot{Indicator: strategy.IndicatorView{NumericValues: map[string]float64{"volume_ratio20": volumeRatio}}}
}

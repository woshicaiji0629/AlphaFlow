package marketregime

import "testing"

func TestV2ScoresSeparateTrendabilityAndDirection(t *testing.T) {
	trendability, direction, reasons := v2Scores(V2Evidence{
		EfficiencyRatio: 0.55, ADX: 32, RibbonSpreadATR: 0.9,
		RibbonExpanding: true, KAMASlopeATR: 0.18,
		Alignment: DirectionLong, MoneyFlow: DirectionLong,
	})
	if trendability < 60 || direction < 60 || len(reasons) == 0 {
		t.Fatalf("trendability=%v direction=%v reasons=%v", trendability, direction, reasons)
	}

	trendability, direction, _ = v2Scores(V2Evidence{
		EfficiencyRatio: 0.05, ADX: 12, RibbonSpreadATR: 0.1,
		NoiseGateNeutral: true,
	})
	if trendability > 35 || direction != 0 {
		t.Fatalf("range trendability=%v direction=%v", trendability, direction)
	}
}

func TestV2DirectionalRibbonBlocksCountertrend(t *testing.T) {
	analyzer, err := NewV2Analyzer(DefaultV2Config())
	if err != nil {
		t.Fatal(err)
	}
	evidence := V2Evidence{
		RibbonSpreadATR: 0.6, RibbonExpanding: true,
		Alignment: DirectionLong,
	}
	state, direction := analyzer.target(45, 10, evidence)
	if state != StateNormal || direction != DirectionLong {
		t.Fatalf("state=%q direction=%q", state, direction)
	}
	_, allowLong, allowShort := v2Permissions(state, direction)
	if !allowLong || allowShort {
		t.Fatalf("allowLong=%t allowShort=%t", allowLong, allowShort)
	}
}

func TestV2TransitionRequiresConfirmation(t *testing.T) {
	config := DefaultV2Config()
	config.ConfirmBars = 2
	analyzer, err := NewV2Analyzer(config)
	if err != nil {
		t.Fatal(err)
	}
	analyzer.transition(StateTrendActive, DirectionShort)
	if analyzer.state != StateNormal {
		t.Fatalf("state changed before confirmation: %q", analyzer.state)
	}
	analyzer.transition(StateTrendActive, DirectionShort)
	if analyzer.state != StateTrendActive || analyzer.direction != DirectionShort {
		t.Fatalf("state=%q direction=%q", analyzer.state, analyzer.direction)
	}
}

func TestVersionedAnalyzerConstructsV2(t *testing.T) {
	analyzer, err := NewAnalyzer(VersionV2, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if analyzer.Version() != VersionV2 {
		t.Fatalf("version=%q", analyzer.Version())
	}
}

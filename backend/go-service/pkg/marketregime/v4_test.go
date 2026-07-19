package marketregime

import "testing"

func TestV4RejectsUnclearDirection(t *testing.T) {
	analyzer, err := NewV4Analyzer(DefaultV4Config())
	if err != nil {
		t.Fatal(err)
	}
	state, direction, _ := analyzer.target(Result{State: StateNormal, Direction: DirectionNone, TrendabilityScore: 50}, v4PhaseEvidence{widthReady: true, widthPercentile: 0.5, widthState: "stable"})
	allowNew, allowLong, allowShort := v4Permissions(state, direction)
	if allowNew || allowLong || allowShort {
		t.Fatalf("unclear direction permissions=(%t,%t,%t)", allowNew, allowLong, allowShort)
	}
}

func TestV4LocksCompressionWithIndependentVotes(t *testing.T) {
	analyzer, err := NewV4Analyzer(DefaultV4Config())
	if err != nil {
		t.Fatal(err)
	}
	state, direction, _ := analyzer.target(Result{State: StateNormal, Direction: DirectionLong, TrendabilityScore: 50}, v4PhaseEvidence{
		widthReady: true, widthPercentile: 0.10, widthState: "contracting",
	})
	if state != StateChopLock || direction != DirectionNone {
		t.Fatalf("state=%q direction=%q", state, direction)
	}
}

func TestV4RequiresReleaseAndBaseDirectionAgreement(t *testing.T) {
	analyzer, err := NewV4Analyzer(DefaultV4Config())
	if err != nil {
		t.Fatal(err)
	}
	evidence := v4PhaseEvidence{
		widthReady: true, widthPercentile: 0.10, widthState: "expanding",
		squeezeState: "release_up", momentum: 1, momentumDelta: 0.5,
	}
	state, direction, _ := analyzer.target(Result{State: StateNormal, Direction: DirectionLong, TrendabilityScore: 20}, evidence)
	if state != StateTrendArmed || direction != DirectionLong {
		t.Fatalf("confirmed state=%q direction=%q", state, direction)
	}
	state, direction, _ = analyzer.target(Result{State: StateNormal, Direction: DirectionShort, TrendabilityScore: 50}, evidence)
	if state != StateBreakoutPending || direction != DirectionNone {
		t.Fatalf("conflict state=%q direction=%q", state, direction)
	}
}

func TestV4BlocksFadingMomentum(t *testing.T) {
	analyzer, err := NewV4Analyzer(DefaultV4Config())
	if err != nil {
		t.Fatal(err)
	}
	state, direction, reasons := analyzer.target(Result{State: StateNormal, Direction: DirectionLong, TrendabilityScore: 50}, v4PhaseEvidence{
		widthReady: true, widthPercentile: 0.5, widthState: "stable", momentumState: "bull_fading",
	})
	if state != StateBreakoutPending || direction != DirectionNone || reasons[len(reasons)-1] != "v4_momentum_unconfirmed" {
		t.Fatalf("state=%q direction=%q reasons=%v", state, direction, reasons)
	}
}

func TestV4SeparatesContractingPhaseReason(t *testing.T) {
	analyzer, err := NewV4Analyzer(DefaultV4Config())
	if err != nil {
		t.Fatal(err)
	}
	base := Result{State: StateNormal, Direction: DirectionLong, TrendabilityScore: 50}
	state, direction, reasons := analyzer.target(base, v4PhaseEvidence{widthReady: true, widthPercentile: 0.5, widthState: "contracting"})
	if state != StateBreakoutPending || direction != DirectionNone || reasons[len(reasons)-1] != "v4_bb_contracting_phase" {
		t.Fatalf("state=%q direction=%q reasons=%v", state, direction, reasons)
	}
}

func TestV4GateReasonReportsConfirmationDelay(t *testing.T) {
	if got := v4GateReason(StateNormal, DirectionNone, StateTrendActive, DirectionLong, []string{"v4_trend_permitted"}); got != "v4_state_pending" {
		t.Fatalf("reason=%q", got)
	}
}

func TestVersionedAnalyzerConstructsV4(t *testing.T) {
	analyzer, err := NewAnalyzer(VersionV4, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if analyzer.Version() != VersionV4 {
		t.Fatalf("version=%q", analyzer.Version())
	}
}

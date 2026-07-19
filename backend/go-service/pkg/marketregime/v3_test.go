package marketregime

import "testing"

func TestV3VolatilityFilterDoesNotInheritDirectionWhileFlat(t *testing.T) {
	analyzer, err := NewV3Analyzer(DefaultV3Config())
	if err != nil {
		t.Fatal(err)
	}
	analyzer.updateFilter(100, 1)
	analyzer.updateFilter(103, 1)
	if analyzer.filterState != VolatilityFilterUp {
		t.Fatalf("state=%q", analyzer.filterState)
	}
	analyzer.updateFilter(analyzer.filterLine, 1)
	if analyzer.filterState != VolatilityFilterFlat {
		t.Fatalf("flat bar inherited direction: %q", analyzer.filterState)
	}
}

func TestV3RequiresNoiseGateAndBaseDirectionAgreement(t *testing.T) {
	analyzer, err := NewV3Analyzer(DefaultV3Config())
	if err != nil {
		t.Fatal(err)
	}
	analyzer.filterState = VolatilityFilterUp
	state, direction, _ := analyzer.target(Result{State: StateTrendActive, Direction: DirectionLong})
	if state != StateTrendActive || direction != DirectionLong {
		t.Fatalf("confirmed state=%q direction=%q", state, direction)
	}
	state, direction, _ = analyzer.target(Result{State: StateTrendActive, Direction: DirectionShort})
	if state != StateBreakoutPending || direction != DirectionNone {
		t.Fatalf("conflict state=%q direction=%q", state, direction)
	}
}

func TestV3FlatFilterPreservesIndependentBaseRegime(t *testing.T) {
	analyzer, err := NewV3Analyzer(DefaultV3Config())
	if err != nil {
		t.Fatal(err)
	}
	analyzer.filterState = VolatilityFilterFlat
	state, direction, _ := analyzer.target(Result{State: StateTrendActive, Direction: DirectionLong})
	if state != StateTrendActive || direction != DirectionLong {
		t.Fatalf("state=%q direction=%q", state, direction)
	}
	state, direction, _ = analyzer.target(Result{State: StateChopLock, Direction: DirectionNone})
	allowNew, allowLong, allowShort := v2Permissions(state, direction)
	if allowNew || allowLong || allowShort {
		t.Fatalf("chop permissions=(%t,%t,%t)", allowNew, allowLong, allowShort)
	}
}

func TestVersionedAnalyzerConstructsV3(t *testing.T) {
	analyzer, err := NewAnalyzer(VersionV3, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if analyzer.Version() != VersionV3 {
		t.Fatalf("version=%q", analyzer.Version())
	}
}

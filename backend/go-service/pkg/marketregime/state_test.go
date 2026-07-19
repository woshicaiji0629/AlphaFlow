package marketregime

import "testing"

func TestVersionedAnalyzerConstructsV1(t *testing.T) {
	analyzer, err := NewAnalyzer(VersionV1, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if analyzer.Version() != VersionV1 {
		t.Fatalf("version=%q", analyzer.Version())
	}
	if _, err := NewAnalyzer(Version("unknown"), DefaultConfig()); err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestPermissionsSeparateLongAndShort(t *testing.T) {
	tests := []struct {
		name                  string
		state                 State
		direction             Direction
		allowNew, long, short bool
	}{
		{name: "normal", state: StateNormal, direction: DirectionNone, allowNew: true, long: true, short: true},
		{name: "long trend", state: StateTrendActive, direction: DirectionLong, allowNew: true, long: true},
		{name: "short trend", state: StateTrendArmed, direction: DirectionShort, allowNew: true, short: true},
		{name: "chop", state: StateChopLock, direction: DirectionNone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allowNew, long, short := permissions(test.state, test.direction)
			if allowNew != test.allowNew || long != test.long || short != test.short {
				t.Fatalf("permissions=(%t,%t,%t)", allowNew, long, short)
			}
		})
	}
}

func TestChopLockBreakoutAndTrendLifecycle(t *testing.T) {
	config := DefaultConfig()
	config.LockConfirmBars = 2
	config.BreakoutConfirmBars = 2
	detector, err := NewDetector(config)
	if err != nil {
		t.Fatal(err)
	}
	detector.transition(100, 110, 90, 1, true, false)
	if detector.state != StateChopWatch {
		t.Fatalf("first dormant state=%q", detector.state)
	}
	detector.transition(100, 110, 90, 1, true, false)
	if detector.state != StateChopLock {
		t.Fatalf("confirmed dormant state=%q", detector.state)
	}
	detector.transition(111, 110, 90, 2, true, false)
	if detector.state != StateBreakoutPending || detector.direction != DirectionLong {
		t.Fatalf("breakout state=%q direction=%q", detector.state, detector.direction)
	}
	detector.transition(112, 110, 90, 1, false, true)
	if detector.state != StateTrendArmed {
		t.Fatalf("confirmed breakout state=%q", detector.state)
	}
	detector.transition(113, 110, 90, 1, false, true)
	if detector.state != StateTrendActive {
		t.Fatalf("active trend state=%q", detector.state)
	}
}

func TestFailedBreakoutReturnsToLockAfterCooldown(t *testing.T) {
	config := DefaultConfig()
	config.FailedBreakoutBars = 2
	detector, err := NewDetector(config)
	if err != nil {
		t.Fatal(err)
	}
	detector.state = StateBreakoutPending
	detector.direction = DirectionShort
	detector.breakoutLow = 90
	detector.transition(91, 110, 90, 1, true, false)
	if detector.state != StateFailedBreakout || detector.failedBreakouts != 1 {
		t.Fatalf("failed state=%q failures=%d", detector.state, detector.failedBreakouts)
	}
	detector.transition(92, 110, 90, 1, true, false)
	if detector.state != StateChopLock || detector.direction != DirectionNone {
		t.Fatalf("post cooldown state=%q direction=%q", detector.state, detector.direction)
	}
}

func TestChopLockExitsWhenDormancyDisappears(t *testing.T) {
	config := DefaultConfig()
	config.UnlockEvidenceBars = 2
	detector, err := NewDetector(config)
	if err != nil {
		t.Fatal(err)
	}
	detector.state = StateChopLock
	detector.transition(100, 110, 90, 1, false, false)
	detector.transition(100, 110, 90, 1, false, false)
	if detector.state != StateNormal {
		t.Fatalf("released state=%q", detector.state)
	}
}

func TestInvalidConfig(t *testing.T) {
	config := DefaultConfig()
	config.HigherIntervals = nil
	if _, err := NewDetector(config); err == nil {
		t.Fatal("expected invalid config error")
	}
}

func TestBreakoutSupportedRequiresDirectionHistogramAndAxis(t *testing.T) {
	evidence := []IntervalEvidence{{
		Available: true, MomentumDirection: DirectionLong,
		MACDAxisATR: 0.4, MACDHistogramATR: 0.1,
	}}
	if !breakoutSupported(evidence, DirectionLong, 0.3, 0.08) {
		t.Fatal("expected long breakout support")
	}
	if breakoutSupported(evidence, DirectionShort, 0.3, 0.08) {
		t.Fatal("opposite direction must not be supported")
	}
	if breakoutSupported(evidence, DirectionLong, 0.5, 0.08) {
		t.Fatal("axis below threshold must not be supported")
	}
}

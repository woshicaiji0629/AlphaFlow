package marketregime

import (
	"fmt"
	"math"

	"alphaflow/go-service/pkg/strategy"
)

// V5Config preserves the v4 entry gate and adds a narrowly scoped fast unlock
// for directional expansion out of a recent compression range.
type V5Config struct {
	V4                V4Config
	BreakoutWindow    int
	MinVolumeRatio    float64
	MinDirectionScore float64
}

func DefaultV5Config() V5Config {
	return V5Config{
		V4: DefaultV4Config(), BreakoutWindow: 20,
		MinVolumeRatio: 1.2, MinDirectionScore: DefaultV2Config().MinDirectionScore,
	}
}

type V5Analyzer struct {
	config V5Config
	gate   *V4Analyzer
	bars   []priceBar
}

func NewV5Analyzer(config V5Config) (*V5Analyzer, error) {
	if config.BreakoutWindow < 3 || config.MinVolumeRatio <= 0 || config.MinDirectionScore <= 0 || config.MinDirectionScore > 100 {
		return nil, fmt.Errorf("invalid market regime v5 config")
	}
	gate, err := NewV4Analyzer(config.V4)
	if err != nil {
		return nil, err
	}
	return &V5Analyzer{config: config, gate: gate}, nil
}

func (a *V5Analyzer) Version() Version { return VersionV5 }

func (a *V5Analyzer) Analyze(snapshot strategy.Snapshot) (Result, bool, error) {
	bar, err := parseBar(snapshot)
	if err != nil {
		return Result{}, false, err
	}
	base, ready, err := a.gate.base.Analyze(snapshot)
	if err != nil || !ready {
		a.appendBar(bar)
		return Result{}, ready, err
	}
	evidence, ready := a.gate.phaseEvidence(snapshot)
	if !ready {
		a.appendBar(bar)
		return Result{}, false, nil
	}
	targetState, targetDirection, phaseReasons := a.target(snapshot, bar, base, evidence)
	if len(phaseReasons) > 0 && phaseReasons[len(phaseReasons)-1] == "v5_fast_release_confirmed" {
		a.gate.state, a.gate.direction = targetState, targetDirection
		a.gate.pendingState, a.gate.pendingDirection, a.gate.pendingBars = "", "", 0
		a.gate.stateBars = 1
	} else {
		a.gate.transition(targetState, targetDirection)
	}
	allowNew, allowLong, allowShort := v4Permissions(a.gate.state, a.gate.direction)
	base.Version = VersionV5
	base.State = a.gate.state
	base.Direction = a.gate.direction
	base.AllowNewPosition = allowNew
	base.AllowLong = allowLong
	base.AllowShort = allowShort
	base.StateBars = a.gate.stateBars
	base.Reasons = append(append([]string(nil), base.Reasons...), phaseReasons...)
	base.Reasons = append(base.Reasons, v5GateReason(a.gate.state, a.gate.direction, targetState, targetDirection, phaseReasons))
	a.appendBar(bar)
	return base, true, nil
}

func (a *V5Analyzer) target(snapshot strategy.Snapshot, bar priceBar, base Result, evidence v4PhaseEvidence) (State, Direction, []string) {
	return a.targetWithWidthExpansion(snapshot, bar, base, evidence, evidence.widthState == "expanding")
}

func (a *V5Analyzer) targetWithWidthExpansion(snapshot strategy.Snapshot, bar priceBar, base Result, evidence v4PhaseEvidence, widthExpanding bool) (State, Direction, []string) {
	breakoutDirection := a.breakoutDirection(bar.close)
	locked := a.gate.state == StateChopLock || base.State == StateChopLock
	if !locked || breakoutDirection == DirectionNone {
		return a.gate.target(base, evidence)
	}
	if !widthExpanding {
		return StateChopLock, DirectionNone, []string{"v5_breakout_width_weak"}
	}
	if !v5DirectionAligned(breakoutDirection, base.DirectionScore, a.config.MinDirectionScore) {
		return StateChopLock, DirectionNone, []string{"v5_breakout_direction_weak"}
	}
	if !v5MomentumAligned(breakoutDirection, evidence.momentum, evidence.momentumDelta) {
		return StateChopLock, DirectionNone, []string{"v5_breakout_momentum_weak"}
	}
	volumeRatio, ok := snapshot.Indicator.Float("volume_ratio20")
	if !ok || volumeRatio < a.config.MinVolumeRatio {
		return StateChopLock, DirectionNone, []string{"v5_breakout_volume_weak"}
	}
	return StateTrendArmed, breakoutDirection, []string{"v5_fast_release_confirmed"}
}

func (a *V5Analyzer) breakoutDirection(closePrice float64) Direction {
	if len(a.bars) < a.config.BreakoutWindow {
		return DirectionNone
	}
	window := a.bars[len(a.bars)-a.config.BreakoutWindow:]
	high, low := window[0].high, window[0].low
	for _, bar := range window[1:] {
		high = math.Max(high, bar.high)
		low = math.Min(low, bar.low)
	}
	if closePrice > high {
		return DirectionLong
	}
	if closePrice < low {
		return DirectionShort
	}
	return DirectionNone
}

func (a *V5Analyzer) appendBar(bar priceBar) {
	a.bars = append(a.bars, bar)
	if len(a.bars) > a.config.BreakoutWindow {
		a.bars = append([]priceBar(nil), a.bars[len(a.bars)-a.config.BreakoutWindow:]...)
	}
}

func v5DirectionAligned(direction Direction, score float64, minimum float64) bool {
	return direction == DirectionLong && score >= minimum || direction == DirectionShort && score <= -minimum
}

func v5MomentumAligned(direction Direction, momentum float64, delta float64) bool {
	return direction == DirectionLong && momentum > 0 && delta > 0 || direction == DirectionShort && momentum < 0 && delta < 0
}

func v5GateReason(state State, direction Direction, targetState State, targetDirection Direction, phaseReasons []string) string {
	if len(phaseReasons) > 0 && phaseReasons[len(phaseReasons)-1] == "v5_fast_release_confirmed" {
		if targetState == StateTrendArmed && targetDirection != DirectionNone {
			return "v5_fast_release_confirmed"
		}
	}
	if state != targetState || direction != targetDirection {
		return "v5_state_pending"
	}
	allowNew, _, _ := v4Permissions(state, direction)
	if allowNew {
		return "v5_permitted"
	}
	if len(phaseReasons) > 0 {
		return phaseReasons[len(phaseReasons)-1]
	}
	return "v5_blocked"
}

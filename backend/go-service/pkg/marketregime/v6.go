package marketregime

import (
	"fmt"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

// V6 keeps v5's breakout protections but accepts continuous width growth,
// allowing a blocked flip to be activated after volatility expansion begins.
type V6Config struct {
	V5 V5Config
}

func DefaultV6Config() V6Config { return V6Config{V5: DefaultV5Config()} }

type V6Analyzer struct {
	config V6Config
	gate   *V5Analyzer
}

func NewV6Analyzer(config V6Config) (*V6Analyzer, error) {
	gate, err := NewV5Analyzer(config.V5)
	if err != nil {
		return nil, fmt.Errorf("build market regime v6: %w", err)
	}
	return &V6Analyzer{config: config, gate: gate}, nil
}

func (a *V6Analyzer) Version() Version { return VersionV6 }

func (a *V6Analyzer) Analyze(snapshot strategy.Snapshot) (Result, bool, error) {
	bar, err := parseBar(snapshot)
	if err != nil {
		return Result{}, false, err
	}
	base, ready, err := a.gate.gate.base.Analyze(snapshot)
	if err != nil || !ready {
		a.gate.appendBar(bar)
		return Result{}, ready, err
	}
	evidence, ready := a.gate.gate.phaseEvidence(snapshot)
	if !ready {
		a.gate.appendBar(bar)
		return Result{}, false, nil
	}
	widthExpanding := evidence.widthState == "expanding"
	if widthSeries, ok := snapshot.Window.Numeric("bb_width_pct"); ok {
		widthExpanding = widthExpanding || widthSeries.RisingCount >= 2
	}
	if widthDelta, ok := snapshot.Indicator.Float("bb_width_delta"); ok {
		widthExpanding = widthExpanding || widthDelta > 0
	}
	targetState, targetDirection, phaseReasons := a.gate.targetWithWidthExpansion(snapshot, bar, base, evidence, widthExpanding)
	phaseReasons = v6Reasons(phaseReasons)
	if len(phaseReasons) > 0 && phaseReasons[len(phaseReasons)-1] == "v6_fast_release_confirmed" {
		a.gate.gate.state, a.gate.gate.direction = targetState, targetDirection
		a.gate.gate.pendingState, a.gate.gate.pendingDirection, a.gate.gate.pendingBars = "", "", 0
		a.gate.gate.stateBars = 1
	} else {
		a.gate.gate.transition(targetState, targetDirection)
	}
	allowNew, allowLong, allowShort := v4Permissions(a.gate.gate.state, a.gate.gate.direction)
	base.Version = VersionV6
	base.State = a.gate.gate.state
	base.Direction = a.gate.gate.direction
	base.AllowNewPosition = allowNew
	base.AllowLong = allowLong
	base.AllowShort = allowShort
	base.StateBars = a.gate.gate.stateBars
	base.Reasons = append(append([]string(nil), base.Reasons...), phaseReasons...)
	base.Reasons = append(base.Reasons, v6GateReason(a.gate.gate.state, a.gate.gate.direction, targetState, targetDirection, phaseReasons))
	a.gate.appendBar(bar)
	return base, true, nil
}

func v6Reasons(reasons []string) []string {
	result := append([]string(nil), reasons...)
	for index, reason := range result {
		if strings.HasPrefix(reason, "v5_") {
			result[index] = "v6_" + strings.TrimPrefix(reason, "v5_")
		}
	}
	return result
}

func v6GateReason(state State, direction Direction, targetState State, targetDirection Direction, reasons []string) string {
	if len(reasons) > 0 && reasons[len(reasons)-1] == "v6_fast_release_confirmed" {
		return "v6_fast_release_confirmed"
	}
	if state != targetState || direction != targetDirection {
		return "v6_state_pending"
	}
	allowNew, _, _ := v4Permissions(state, direction)
	if allowNew {
		return "v6_permitted"
	}
	if len(reasons) > 0 {
		return reasons[len(reasons)-1]
	}
	return "v6_blocked"
}

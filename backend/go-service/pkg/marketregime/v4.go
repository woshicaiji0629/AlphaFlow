package marketregime

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

// V4Config adds an entry-phase gate to the frozen v2 direction model. It uses
// existing Bollinger and Squeeze Momentum outputs; it does not create a second
// direction model or authorize entries by itself.
type V4Config struct {
	V2                    V2Config
	WidthWindow           int
	CompressionPercentile float64
	MinCompressionVotes   int
	ConfirmBars           int
}

func DefaultV4Config() V4Config {
	return V4Config{
		V2: DefaultV2Config(), WidthWindow: 100,
		CompressionPercentile: 0.25, MinCompressionVotes: 2,
		ConfirmBars: 2,
	}
}

type v4PhaseEvidence struct {
	widthPercentile float64
	widthReady      bool
	widthState      string
	squeezeState    string
	momentumState   string
	momentum        float64
	momentumDelta   float64
}

type V4Analyzer struct {
	config           V4Config
	base             *V2Analyzer
	widths           []float64
	state            State
	direction        Direction
	pendingState     State
	pendingDirection Direction
	pendingBars      int
	stateBars        int
}

func NewV4Analyzer(config V4Config) (*V4Analyzer, error) {
	if config.WidthWindow < 20 || config.CompressionPercentile <= 0 || config.CompressionPercentile >= 1 ||
		config.MinCompressionVotes < 1 || config.MinCompressionVotes > 4 || config.ConfirmBars <= 0 {
		return nil, fmt.Errorf("invalid market regime v4 config")
	}
	base, err := NewV2Analyzer(config.V2)
	if err != nil {
		return nil, err
	}
	return &V4Analyzer{config: config, base: base, state: StateNormal}, nil
}

func (a *V4Analyzer) Version() Version { return VersionV4 }

func (a *V4Analyzer) Analyze(snapshot strategy.Snapshot) (Result, bool, error) {
	base, ready, err := a.base.Analyze(snapshot)
	if err != nil || !ready {
		return Result{}, ready, err
	}
	evidence, ready := a.phaseEvidence(snapshot)
	if !ready {
		return Result{}, false, nil
	}
	targetState, targetDirection, phaseReasons := a.target(base, evidence)
	a.transition(targetState, targetDirection)
	allowNew, allowLong, allowShort := v4Permissions(a.state, a.direction)
	base.Version = VersionV4
	base.State = a.state
	base.Direction = a.direction
	base.AllowNewPosition = allowNew
	base.AllowLong = allowLong
	base.AllowShort = allowShort
	base.StateBars = a.stateBars
	base.Reasons = append(append([]string(nil), base.Reasons...), phaseReasons...)
	base.Reasons = append(base.Reasons, v4GateReason(a.state, a.direction, targetState, targetDirection, phaseReasons))
	return base, true, nil
}

func (a *V4Analyzer) phaseEvidence(snapshot strategy.Snapshot) (v4PhaseEvidence, bool) {
	width, ok := snapshot.Indicator.Float("bb_width_pct")
	if !ok || math.IsNaN(width) || math.IsInf(width, 0) || width < 0 {
		return v4PhaseEvidence{}, false
	}
	a.widths = append(a.widths, width)
	if len(a.widths) > a.config.WidthWindow {
		copy(a.widths, a.widths[len(a.widths)-a.config.WidthWindow:])
		a.widths = a.widths[:a.config.WidthWindow]
	}
	evidence := v4PhaseEvidence{
		widthState:    normalizeV4Signal(snapshot.Indicator.Signals["bb_width_state"]),
		squeezeState:  normalizeV4Signal(snapshot.Indicator.Signals["squeeze_state"]),
		momentumState: normalizeV4Signal(snapshot.Indicator.Signals["momentum_state"]),
	}
	evidence.momentum, _ = snapshot.Indicator.Float("squeeze_momentum")
	evidence.momentumDelta, _ = snapshot.Indicator.Float("squeeze_momentum_delta")
	if len(a.widths) < a.config.WidthWindow {
		return evidence, false
	}
	evidence.widthPercentile = percentileRank(a.widths, width)
	evidence.widthReady = true
	return evidence, true
}

func (a *V4Analyzer) target(base Result, evidence v4PhaseEvidence) (State, Direction, []string) {
	reasons := make([]string, 0, 6)
	releaseDirection := v4ReleaseDirection(evidence)
	if releaseDirection != DirectionNone {
		if base.Direction == releaseDirection {
			return StateTrendArmed, releaseDirection, append(reasons, "v4_release_confirmed")
		}
		return StateBreakoutPending, DirectionNone, append(reasons, "v4_release_unconfirmed")
	}
	compressionVotes := 0
	if evidence.widthReady && evidence.widthPercentile <= a.config.CompressionPercentile {
		compressionVotes++
		reasons = append(reasons, "v4_bb_low_percentile")
	}
	if evidence.widthState == "contracting" {
		compressionVotes++
		reasons = append(reasons, "v4_bb_contracting")
	}
	if evidence.squeezeState == "squeeze_on" {
		compressionVotes++
		reasons = append(reasons, "v4_squeeze_on")
	}
	if base.TrendabilityScore <= a.config.V2.RangeMaxTrendability {
		compressionVotes++
		reasons = append(reasons, "v4_low_trendability")
	}
	if base.State == StateChopLock || compressionVotes >= a.config.MinCompressionVotes {
		return StateChopLock, DirectionNone, append(reasons, "v4_compression_locked")
	}

	if base.Direction == DirectionNone {
		return StateNormal, DirectionNone, append(reasons, "v4_direction_unclear")
	}
	if evidence.widthState == "contracting" {
		return StateBreakoutPending, DirectionNone, append(reasons, "v4_bb_contracting_phase")
	}
	if v4MomentumFading(base.Direction, evidence.momentumState) {
		return StateBreakoutPending, DirectionNone, append(reasons, "v4_momentum_unconfirmed")
	}
	return base.State, base.Direction, append(reasons, "v4_trend_permitted")
}

func v4ReleaseDirection(evidence v4PhaseEvidence) Direction {
	switch evidence.squeezeState {
	case "release_up":
		if evidence.momentum > 0 && evidence.momentumDelta > 0 && evidence.widthState == "expanding" {
			return DirectionLong
		}
	case "release_down":
		if evidence.momentum < 0 && evidence.momentumDelta < 0 && evidence.widthState == "expanding" {
			return DirectionShort
		}
	}
	return DirectionNone
}

func v4MomentumFading(direction Direction, state string) bool {
	return direction == DirectionLong && (state == "bear" || state == "bull_fading") ||
		direction == DirectionShort && (state == "bull" || state == "bear_fading")
}

func v4GateReason(state State, direction Direction, targetState State, targetDirection Direction, phaseReasons []string) string {
	if state != targetState || direction != targetDirection {
		return "v4_state_pending"
	}
	allowNew, _, _ := v4Permissions(state, direction)
	if allowNew {
		return "v4_permitted"
	}
	if len(phaseReasons) > 0 {
		return phaseReasons[len(phaseReasons)-1]
	}
	return "v4_blocked"
}

func v4Permissions(state State, direction Direction) (bool, bool, bool) {
	if state == StateChopLock || state == StateBreakoutPending || direction == DirectionNone {
		return false, false, false
	}
	if direction == DirectionLong {
		return true, true, false
	}
	return true, false, true
}

func (a *V4Analyzer) transition(state State, direction Direction) {
	if state == a.state && direction == a.direction {
		a.pendingBars = 0
		a.stateBars++
		return
	}
	if state != a.pendingState || direction != a.pendingDirection {
		a.pendingState, a.pendingDirection, a.pendingBars = state, direction, 1
		a.stateBars++
		return
	}
	a.pendingBars++
	if a.pendingBars >= a.config.ConfirmBars {
		a.state, a.direction = state, direction
		a.pendingBars, a.stateBars = 0, 1
	} else {
		a.stateBars++
	}
}

func percentileRank(values []float64, current float64) float64 {
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	index := sort.SearchFloat64s(ordered, current)
	for index+1 < len(ordered) && ordered[index+1] <= current {
		index++
	}
	return float64(index+1) / float64(len(ordered))
}

func normalizeV4Signal(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

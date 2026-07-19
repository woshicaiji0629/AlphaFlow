package marketregime

import (
	"fmt"
	"math"

	"alphaflow/go-service/pkg/strategy"
)

type VolatilityFilterState string

const (
	VolatilityFilterFlat VolatilityFilterState = "flat"
	VolatilityFilterUp   VolatilityFilterState = "up"
	VolatilityFilterDown VolatilityFilterState = "down"
)

// V3Config adds a causal ATR noise gate to v2. The implementation is an
// independent state machine based on the general ATR dead-band technique.
type V3Config struct {
	V2              V2Config
	NoiseMultiplier float64
	CatchUpSpeed    float64
	ConfirmBars     int
}

func DefaultV3Config() V3Config {
	return V3Config{
		V2: DefaultV2Config(), NoiseMultiplier: 1.5,
		CatchUpSpeed: 0.35, ConfirmBars: 2,
	}
}

type V3Analyzer struct {
	config           V3Config
	base             *V2Analyzer
	filterLine       float64
	filterReady      bool
	filterState      VolatilityFilterState
	state            State
	direction        Direction
	pendingState     State
	pendingDirection Direction
	pendingBars      int
	stateBars        int
}

func NewV3Analyzer(config V3Config) (*V3Analyzer, error) {
	if config.NoiseMultiplier <= 0 || config.CatchUpSpeed <= 0 || config.CatchUpSpeed > 1 || config.ConfirmBars <= 0 {
		return nil, fmt.Errorf("invalid market regime v3 config")
	}
	base, err := NewV2Analyzer(config.V2)
	if err != nil {
		return nil, err
	}
	return &V3Analyzer{config: config, base: base, state: StateNormal}, nil
}

func (a *V3Analyzer) Version() Version { return VersionV3 }

func (a *V3Analyzer) Analyze(snapshot strategy.Snapshot) (Result, bool, error) {
	bar, err := parseBar(snapshot)
	if err != nil {
		return Result{}, false, err
	}
	atr, ok := snapshot.Indicator.Float("atr14")
	if !ok || atr <= 0 {
		return Result{}, false, nil
	}
	a.updateFilter(bar.close, atr)
	base, ready, err := a.base.Analyze(snapshot)
	if err != nil || !ready {
		return Result{}, ready, err
	}
	targetState, targetDirection, reason := a.target(base)
	a.transition(targetState, targetDirection)
	allowNew, allowLong, allowShort := v2Permissions(a.state, a.direction)
	reasons := append([]string(nil), base.Reasons...)
	reasons = append(reasons, reason)
	base.Version = VersionV3
	base.State = a.state
	base.Direction = a.direction
	base.AllowNewPosition = allowNew
	base.AllowLong = allowLong
	base.AllowShort = allowShort
	base.StateBars = a.stateBars
	base.Reasons = reasons
	return base, true, nil
}

func (a *V3Analyzer) updateFilter(price float64, atr float64) {
	if !a.filterReady {
		a.filterLine, a.filterReady, a.filterState = price, true, VolatilityFilterFlat
		return
	}
	previous := a.filterLine
	difference := price - a.filterLine
	if math.Abs(difference) > atr*a.config.NoiseMultiplier {
		a.filterLine += difference * a.config.CatchUpSpeed
	}
	switch {
	case a.filterLine > previous:
		a.filterState = VolatilityFilterUp
	case a.filterLine < previous:
		a.filterState = VolatilityFilterDown
	default:
		a.filterState = VolatilityFilterFlat
	}
}

func (a *V3Analyzer) target(base Result) (State, Direction, string) {
	switch a.filterState {
	case VolatilityFilterFlat:
		// A flat ATR filter is one item of range evidence, not a global veto.
		// Preserve v2's independently confirmed regime and direction.
		return base.State, base.Direction, "volatility_filter_flat"
	case VolatilityFilterUp:
		if base.Direction == DirectionLong {
			return base.State, DirectionLong, "volatility_filter_up_confirmed"
		}
		if base.Direction == DirectionNone && base.State != StateChopLock {
			return StateNormal, DirectionLong, "volatility_filter_up_leading"
		}
		return StateBreakoutPending, DirectionNone, "volatility_filter_up_unconfirmed"
	case VolatilityFilterDown:
		if base.Direction == DirectionShort {
			return base.State, DirectionShort, "volatility_filter_down_confirmed"
		}
		if base.Direction == DirectionNone && base.State != StateChopLock {
			return StateNormal, DirectionShort, "volatility_filter_down_leading"
		}
		return StateBreakoutPending, DirectionNone, "volatility_filter_down_unconfirmed"
	default:
		return StateChopLock, DirectionNone, "volatility_filter_unknown"
	}
}

func (a *V3Analyzer) transition(state State, direction Direction) {
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

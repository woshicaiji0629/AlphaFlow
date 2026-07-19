package marketregime

import (
	"fmt"
	"math"

	"alphaflow/go-service/pkg/strategy"
)

// V2Config separates market trendability from direction. Thresholds are
// normalized by ATR so the model is portable across symbols and price levels.
type V2Config struct {
	HigherInterval          string
	EfficiencyWindow        int
	RangeMaxTrendability    float64
	TrendMinTrendability    float64
	MinDirectionScore       float64
	MinDirectionalRibbonATR float64
	ConfirmBars             int
}

func DefaultV2Config() V2Config {
	return V2Config{
		HigherInterval: "15m", EfficiencyWindow: 20,
		RangeMaxTrendability: 35, TrendMinTrendability: 60,
		MinDirectionScore: 35, MinDirectionalRibbonATR: 0.30,
		ConfirmBars: 2,
	}
}

type V2Evidence struct {
	EfficiencyRatio  float64
	ADX              float64
	RibbonSpreadATR  float64
	RibbonExpanding  bool
	KAMASlopeATR     float64
	NoiseGateNeutral bool
	Alignment        Direction
	MoneyFlow        Direction
}

type V2Analyzer struct {
	config           V2Config
	bars             []priceBar
	state            State
	direction        Direction
	pendingState     State
	pendingDirection Direction
	pendingBars      int
	stateBars        int
}

func NewV2Analyzer(config V2Config) (*V2Analyzer, error) {
	if config.HigherInterval == "" || config.EfficiencyWindow < 3 ||
		config.RangeMaxTrendability < 0 || config.TrendMinTrendability > 100 ||
		config.RangeMaxTrendability >= config.TrendMinTrendability ||
		config.MinDirectionScore <= 0 || config.MinDirectionScore > 100 ||
		config.MinDirectionalRibbonATR <= 0 || config.ConfirmBars <= 0 {
		return nil, fmt.Errorf("invalid market regime v2 config")
	}
	return &V2Analyzer{config: config, state: StateNormal}, nil
}

func (a *V2Analyzer) Version() Version { return VersionV2 }

func (a *V2Analyzer) Analyze(snapshot strategy.Snapshot) (Result, bool, error) {
	bar, err := parseBar(snapshot)
	if err != nil {
		return Result{}, false, err
	}
	a.bars = append(a.bars, bar)
	if len(a.bars) > a.config.EfficiencyWindow+1 {
		a.bars = append([]priceBar(nil), a.bars[len(a.bars)-a.config.EfficiencyWindow-1:]...)
	}
	if len(a.bars) < a.config.EfficiencyWindow+1 {
		return Result{}, false, nil
	}
	timeframe, ok := snapshot.Timeframes[a.config.HigherInterval]
	if !ok {
		return Result{}, false, fmt.Errorf("market regime v2 requires timeframe %q", a.config.HigherInterval)
	}
	evidence, ok := a.evidence(timeframe)
	if !ok {
		return Result{}, false, nil
	}
	evidence.EfficiencyRatio = efficiencyRatio(a.bars)
	trendability, directionScore, reasons := v2Scores(evidence)
	targetState, targetDirection := a.target(trendability, directionScore, evidence)
	a.transition(targetState, targetDirection)
	allowNew, allowLong, allowShort := v2Permissions(a.state, a.direction)
	confidence := trendability
	if a.state == StateChopLock {
		confidence = 100 - trendability
	}
	return Result{
		Version: VersionV2, State: a.state, Direction: a.direction,
		AllowNewPosition: allowNew, AllowLong: allowLong, AllowShort: allowShort,
		BarCloseTimeMS:    snapshot.Current.CloseTime,
		EfficiencyRatio:   evidence.EfficiencyRatio,
		TrendabilityScore: trendability, DirectionScore: directionScore,
		Confidence: clamp(confidence, 0, 100), Reasons: reasons,
		StateBars: a.stateBars,
		Evidence: []IntervalEvidence{{
			Interval: a.config.HigherInterval, Available: true,
			MASpreadATR: evidence.RibbonSpreadATR, MATangled: evidence.NoiseGateNeutral,
			MomentumDirection: evidence.Alignment,
		}},
	}, true, nil
}

func (a *V2Analyzer) evidence(timeframe strategy.TimeframeSnapshot) (V2Evidence, bool) {
	atr, atrOK := timeframe.Indicator.Float("atr14")
	adx, adxOK := timeframe.Indicator.Float("adx14")
	ema7, ema7OK := timeframe.Indicator.Float("ema7")
	ema25, ema25OK := timeframe.Indicator.Float("ema25")
	ema99, ema99OK := timeframe.Indicator.Float("ema99")
	kama, kamaOK := timeframe.Indicator.Float("kama10")
	kamaSeries, kamaSeriesOK := timeframe.Window.Numeric("kama10")
	spreadSeries, spreadSeriesOK := timeframe.Window.Numeric("ma_group_spread_pct")
	if !atrOK || atr <= 0 || !adxOK || !ema7OK || !ema25OK || !ema99OK || !kamaOK || !kamaSeriesOK || !spreadSeriesOK {
		return V2Evidence{}, false
	}
	result := V2Evidence{
		ADX:             adx,
		RibbonSpreadATR: (max3(ema7, ema25, ema99) - min3(ema7, ema25, ema99)) / atr,
		RibbonExpanding: spreadSeries.Latest > spreadSeries.Previous && spreadSeries.RisingCount >= 2,
		KAMASlopeATR:    (kama - kamaSeries.Previous) / atr,
	}
	switch {
	case ema7 > ema25 && ema25 > ema99:
		result.Alignment = DirectionLong
	case ema7 < ema25 && ema25 < ema99:
		result.Alignment = DirectionShort
	}
	flow := latestSignal(timeframe.Window, "money_flow_window_bias")
	if flow == "bull" {
		result.MoneyFlow = DirectionLong
	} else if flow == "bear" {
		result.MoneyFlow = DirectionShort
	}
	result.NoiseGateNeutral = math.Abs(result.KAMASlopeATR) < 0.05 && result.RibbonSpreadATR < a.config.MinDirectionalRibbonATR
	return result, true
}

func v2Scores(e V2Evidence) (float64, float64, []string) {
	trendability := clamp(e.EfficiencyRatio/0.60, 0, 1)*40 +
		clamp((e.ADX-15)/25, 0, 1)*30 +
		clamp(e.RibbonSpreadATR/1.20, 0, 1)*20
	if e.RibbonExpanding {
		trendability += 10
	}
	if e.NoiseGateNeutral {
		trendability -= 20
	}
	direction := 0.0
	reasons := make([]string, 0, 5)
	if e.Alignment == DirectionLong {
		direction += 40
		reasons = append(reasons, "ribbon_bullish")
	} else if e.Alignment == DirectionShort {
		direction -= 40
		reasons = append(reasons, "ribbon_bearish")
	}
	direction += clamp(e.KAMASlopeATR/0.20, -1, 1) * 35
	if e.MoneyFlow == DirectionLong {
		direction += 15
		reasons = append(reasons, "money_flow_bullish")
	} else if e.MoneyFlow == DirectionShort {
		direction -= 15
		reasons = append(reasons, "money_flow_bearish")
	}
	if e.RibbonExpanding {
		reasons = append(reasons, "ribbon_expanding")
	}
	if e.NoiseGateNeutral {
		reasons = append(reasons, "noise_gate_neutral")
	}
	return clamp(trendability, 0, 100), clamp(direction, -100, 100), reasons
}

func (a *V2Analyzer) target(trendability float64, directionScore float64, evidence V2Evidence) (State, Direction) {
	if trendability <= a.config.RangeMaxTrendability {
		return StateChopLock, DirectionNone
	}
	direction := DirectionNone
	if math.Abs(directionScore) >= a.config.MinDirectionScore {
		if directionScore > 0 {
			direction = DirectionLong
		} else {
			direction = DirectionShort
		}
	}
	directionalRibbon := evidence.RibbonExpanding && evidence.RibbonSpreadATR >= a.config.MinDirectionalRibbonATR && evidence.Alignment != DirectionNone
	if directionalRibbon {
		direction = evidence.Alignment
	}
	if trendability >= a.config.TrendMinTrendability && direction != DirectionNone {
		return StateTrendActive, direction
	}
	return StateNormal, direction
}

func (a *V2Analyzer) transition(state State, direction Direction) {
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

func v2Permissions(state State, direction Direction) (bool, bool, bool) {
	if state == StateChopLock {
		return false, false, false
	}
	switch direction {
	case DirectionLong:
		return true, true, false
	case DirectionShort:
		return true, false, true
	default:
		return true, true, true
	}
}

func clamp(value float64, minimum float64, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

func max3(a float64, b float64, c float64) float64 { return math.Max(a, math.Max(b, c)) }
func min3(a float64, b float64, c float64) float64 { return math.Min(a, math.Min(b, c)) }

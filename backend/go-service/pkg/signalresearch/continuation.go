package signalresearch

import (
	"encoding/json"
	"fmt"
	"math"

	"alphaflow/go-service/pkg/strategy"
)

const (
	VolatilityImpulseSource = "volatility_impulse"
	PullbackResumeSource    = "trend_pullback_resume"
)

type EventGate struct {
	cooldownBars int
	remaining    map[strategy.SignalSide]int
}

func NewEventGate(cooldownBars int) (*EventGate, error) {
	if cooldownBars < 0 {
		return nil, fmt.Errorf("event gate cooldown bars cannot be negative")
	}
	return &EventGate{cooldownBars: cooldownBars, remaining: map[strategy.SignalSide]int{}}, nil
}

func (g *EventGate) Advance() {
	for side, remaining := range g.remaining {
		if remaining > 0 {
			g.remaining[side] = remaining - 1
		}
	}
}

func (g *EventGate) Allow(side strategy.SignalSide, sources []string) bool {
	if len(sources) == 0 || g.remaining[side] > 0 {
		return false
	}
	// Advance is called at the start of each bar, so retain one extra count to
	// suppress the configured number of complete bars after this event.
	g.remaining[side] = g.cooldownBars + 1
	return true
}

type ImpulseConfig struct {
	LookbackBars   int
	BreakoutBars   int
	MinMoveATR     float64
	MinVolumeRatio float64
	CooldownBars   int
}

type ImpulseDetector struct {
	config    ImpulseConfig
	bars      []platformBar
	lastOpen  int64
	cooldowns map[strategy.SignalSide]int
}

func NewImpulseDetector(config ImpulseConfig) (*ImpulseDetector, error) {
	if config.LookbackBars <= 0 || config.BreakoutBars <= 1 || config.MinMoveATR <= 0 || config.MinVolumeRatio <= 0 || config.CooldownBars < 0 {
		return nil, fmt.Errorf("invalid impulse detector config")
	}
	return &ImpulseDetector{config: config, cooldowns: map[strategy.SignalSide]int{}}, nil
}

func (d *ImpulseDetector) Update(snapshot strategy.Snapshot) ([]PlatformEvent, error) {
	if snapshot.Current.OpenTime <= d.lastOpen {
		return nil, nil
	}
	bar, err := parsePlatformBar(snapshot)
	if err != nil {
		return nil, err
	}
	d.lastOpen = snapshot.Current.OpenTime
	for side, remaining := range d.cooldowns {
		if remaining > 0 {
			d.cooldowns[side] = remaining - 1
		}
	}
	need := max(d.config.LookbackBars, d.config.BreakoutBars)
	if len(d.bars) < need {
		d.append(bar, need+1)
		return nil, nil
	}
	atr, ok := snapshot.Indicator.Float("atr14")
	if !ok || atr <= 0 {
		return nil, nil
	}
	volumeRatio, ok := snapshot.Indicator.Float("volume_ratio20")
	if !ok || volumeRatio < d.config.MinVolumeRatio {
		d.append(bar, need+1)
		return nil, nil
	}
	lookbackClose := d.bars[len(d.bars)-d.config.LookbackBars].close
	move := bar.close - lookbackClose
	moveATR := math.Abs(move) / atr
	if moveATR < d.config.MinMoveATR {
		d.append(bar, need+1)
		return nil, nil
	}
	structure := d.bars[len(d.bars)-d.config.BreakoutBars:]
	high, low := structure[0].high, structure[0].low
	for _, item := range structure[1:] {
		high = math.Max(high, item.high)
		low = math.Min(low, item.low)
	}
	side := strategy.SignalSideBuy
	breakout := bar.close > high
	if move < 0 {
		side = strategy.SignalSideSell
		breakout = bar.close < low
	}
	if !breakout || d.cooldowns[side] > 0 {
		d.append(bar, need+1)
		return nil, nil
	}
	metadata := map[string]any{
		"version": "volatility-impulse.v1", "phase": "impulse", "side": side,
		"lookback_bars": d.config.LookbackBars, "breakout_bars": d.config.BreakoutBars,
		"move_pct": move / lookbackClose * 100, "move_atr": moveATR,
		"volume_ratio20": volumeRatio, "atr14": atr, "entry_close": bar.close,
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode impulse metadata: %w", err)
	}
	d.cooldowns[side] = d.config.CooldownBars
	d.append(bar, need+1)
	return []PlatformEvent{{Side: side, Source: VolatilityImpulseSource, MetadataJSON: string(encoded)}}, nil
}

func (d *ImpulseDetector) append(bar platformBar, keep int) {
	d.bars = append(d.bars, bar)
	if len(d.bars) > keep {
		d.bars = append([]platformBar(nil), d.bars[len(d.bars)-keep:]...)
	}
}

type PullbackConfig struct {
	TouchDistancePct float64
	ResumeBars       int
	MaxArmedBars     int
	MinVolumeRatio   float64
	CooldownBars     int
}

type pullbackState struct {
	armed    bool
	age      int
	cooldown int
}

type PullbackDetector struct {
	config   PullbackConfig
	bars     []platformBar
	lastOpen int64
	states   map[strategy.SignalSide]pullbackState
}

func NewPullbackDetector(config PullbackConfig) (*PullbackDetector, error) {
	if config.TouchDistancePct <= 0 || config.ResumeBars <= 0 || config.MaxArmedBars <= 0 || config.MinVolumeRatio <= 0 || config.CooldownBars < 0 {
		return nil, fmt.Errorf("invalid pullback detector config")
	}
	return &PullbackDetector{config: config, states: map[strategy.SignalSide]pullbackState{}}, nil
}

func (d *PullbackDetector) Update(snapshot strategy.Snapshot) ([]PlatformEvent, error) {
	if snapshot.Current.OpenTime <= d.lastOpen {
		return nil, nil
	}
	bar, err := parsePlatformBar(snapshot)
	if err != nil {
		return nil, err
	}
	d.lastOpen = snapshot.Current.OpenTime
	events := make([]PlatformEvent, 0, 1)
	for _, side := range []strategy.SignalSide{strategy.SignalSideBuy, strategy.SignalSideSell} {
		event, ok, err := d.evaluate(snapshot, bar, side)
		if err != nil {
			return nil, err
		}
		if ok {
			events = append(events, event)
		}
	}
	d.bars = append(d.bars, bar)
	if keep := d.config.ResumeBars + 1; len(d.bars) > keep {
		d.bars = append([]platformBar(nil), d.bars[len(d.bars)-keep:]...)
	}
	return events, nil
}

func (d *PullbackDetector) evaluate(snapshot strategy.Snapshot, bar platformBar, side strategy.SignalSide) (PlatformEvent, bool, error) {
	state := d.states[side]
	if state.cooldown > 0 {
		state.cooldown--
		d.states[side] = state
		return PlatformEvent{}, false, nil
	}
	aligned, levels := platformTrendAligned(snapshot, bar.close, side)
	if !aligned {
		d.states[side] = pullbackState{}
		return PlatformEvent{}, false, nil
	}
	ema25, ok := snapshot.Indicator.Float("ema25")
	if !ok || ema25 <= 0 {
		return PlatformEvent{}, false, nil
	}
	distancePct := math.Abs(bar.close-ema25) / ema25 * 100
	touched := distancePct <= d.config.TouchDistancePct || (bar.low <= ema25 && bar.high >= ema25)
	if touched {
		state.armed = true
		state.age = 0
	}
	if !state.armed {
		d.states[side] = state
		return PlatformEvent{}, false, nil
	}
	state.age++
	if state.age > d.config.MaxArmedBars {
		d.states[side] = pullbackState{}
		return PlatformEvent{}, false, nil
	}
	if len(d.bars) < d.config.ResumeBars {
		d.states[side] = state
		return PlatformEvent{}, false, nil
	}
	volumeRatio, ok := snapshot.Indicator.Float("volume_ratio5")
	if !ok || volumeRatio < d.config.MinVolumeRatio {
		d.states[side] = state
		return PlatformEvent{}, false, nil
	}
	window := d.bars[len(d.bars)-d.config.ResumeBars:]
	high, low := window[0].high, window[0].low
	for _, item := range window[1:] {
		high = math.Max(high, item.high)
		low = math.Min(low, item.low)
	}
	resumed := side == strategy.SignalSideBuy && bar.close > high
	if side == strategy.SignalSideSell {
		resumed = bar.close < low
	}
	if !resumed {
		d.states[side] = state
		return PlatformEvent{}, false, nil
	}
	metadata := map[string]any{
		"version": "trend-pullback-resume.v1", "phase": "resume", "side": side,
		"armed_age_bars": state.age, "resume_bars": d.config.ResumeBars,
		"ema25": ema25, "ema25_distance_pct": distancePct, "volume_ratio5": volumeRatio,
		"entry_close": bar.close, "entry_supertrend": levels.entry,
		"confirmation_supertrend": levels.confirmation,
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return PlatformEvent{}, false, fmt.Errorf("encode pullback resume metadata: %w", err)
	}
	state.armed = false
	state.age = 0
	state.cooldown = d.config.CooldownBars
	d.states[side] = state
	return PlatformEvent{Side: side, Source: PullbackResumeSource, MetadataJSON: string(encoded)}, true, nil
}

package signalresearch

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"alphaflow/go-service/pkg/strategy"
)

const PlatformBreakoutSource = "trend_platform_breakout"

type PlatformConfig struct {
	WindowBars     int
	MaxRangePct    float64
	MinVolumeRatio float64
	CooldownBars   int
}

type PlatformEvent struct {
	Side         strategy.SignalSide
	Source       string
	MetadataJSON string
}

type platformBar struct {
	open   float64
	high   float64
	low    float64
	close  float64
	volume float64
}

type platformSideState struct {
	phase             string
	cooldownRemaining int
}

type PlatformDetector struct {
	config       PlatformConfig
	bars         []platformBar
	lastOpenTime int64
	states       map[strategy.SignalSide]platformSideState
}

func NewPlatformDetector(config PlatformConfig) (*PlatformDetector, error) {
	if config.WindowBars <= 1 {
		return nil, fmt.Errorf("platform window bars must be greater than one")
	}
	if config.MaxRangePct <= 0 {
		return nil, fmt.Errorf("platform max range pct must be positive")
	}
	if config.MinVolumeRatio <= 0 {
		return nil, fmt.Errorf("platform minimum volume ratio must be positive")
	}
	if config.CooldownBars < 0 {
		return nil, fmt.Errorf("platform cooldown bars cannot be negative")
	}
	return &PlatformDetector{
		config: config,
		states: map[strategy.SignalSide]platformSideState{
			strategy.SignalSideBuy:  {phase: "inactive"},
			strategy.SignalSideSell: {phase: "inactive"},
		},
	}, nil
}

func (d *PlatformDetector) Update(snapshot strategy.Snapshot) ([]PlatformEvent, error) {
	if snapshot.Current.OpenTime <= d.lastOpenTime {
		return nil, nil
	}
	bar, err := parsePlatformBar(snapshot)
	if err != nil {
		return nil, err
	}
	d.lastOpenTime = snapshot.Current.OpenTime

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
	if keep := d.config.WindowBars + 1; len(d.bars) > keep {
		d.bars = append([]platformBar(nil), d.bars[len(d.bars)-keep:]...)
	}
	return events, nil
}

func (d *PlatformDetector) evaluate(snapshot strategy.Snapshot, current platformBar, side strategy.SignalSide) (PlatformEvent, bool, error) {
	state := d.states[side]
	if state.cooldownRemaining > 0 {
		state.cooldownRemaining--
		state.phase = "consumed"
		d.states[side] = state
		return PlatformEvent{}, false, nil
	}

	aligned, levels := platformTrendAligned(snapshot, current.close, side)
	if !aligned {
		if state.phase != "inactive" {
			state.phase = "invalidated"
		} else {
			state.phase = "inactive"
		}
		d.states[side] = state
		return PlatformEvent{}, false, nil
	}
	if len(d.bars) < d.config.WindowBars {
		state.phase = "building"
		d.states[side] = state
		return PlatformEvent{}, false, nil
	}

	window := d.bars[len(d.bars)-d.config.WindowBars:]
	high, low, volume := window[0].high, window[0].low, 0.0
	for _, bar := range window {
		high = math.Max(high, bar.high)
		low = math.Min(low, bar.low)
		volume += bar.volume
	}
	rangePct := (high - low) / low * 100
	if rangePct > d.config.MaxRangePct {
		state.phase = "building"
		d.states[side] = state
		return PlatformEvent{}, false, nil
	}
	state.phase = "ready"
	averageVolume := volume / float64(len(window))
	volumeRatio := 0.0
	if averageVolume > 0 && current.volume > 0 {
		volumeRatio = current.volume / averageVolume
	}
	breakout := side == strategy.SignalSideBuy && current.close > high
	if side == strategy.SignalSideSell {
		breakout = current.close < low
	}
	if !breakout || volumeRatio < d.config.MinVolumeRatio {
		d.states[side] = state
		return PlatformEvent{}, false, nil
	}

	breakoutPct := (current.close/high - 1) * 100
	if side == strategy.SignalSideSell {
		breakoutPct = (low/current.close - 1) * 100
	}
	metadata := map[string]any{
		"version":                 "trend-platform.v1",
		"phase":                   "breakout",
		"side":                    side,
		"window_bars":             d.config.WindowBars,
		"platform_high":           high,
		"platform_low":            low,
		"platform_range_pct":      rangePct,
		"breakout_pct":            breakoutPct,
		"volume_ratio":            volumeRatio,
		"entry_close":             current.close,
		"entry_supertrend":        levels.entry,
		"confirmation_supertrend": levels.confirmation,
		"entry_direction":         levels.entryDirection,
		"confirmation_direction":  levels.confirmationDirection,
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return PlatformEvent{}, false, fmt.Errorf("encode platform event metadata: %w", err)
	}
	state.phase = "consumed"
	state.cooldownRemaining = d.config.CooldownBars
	d.states[side] = state
	return PlatformEvent{Side: side, Source: PlatformBreakoutSource, MetadataJSON: string(encoded)}, true, nil
}

type platformLevels struct {
	entry                 float64
	confirmation          float64
	entryDirection        string
	confirmationDirection string
}

func platformTrendAligned(snapshot strategy.Snapshot, closePrice float64, side strategy.SignalSide) (bool, platformLevels) {
	entryDirection := indicatorSignal(snapshot.Indicator, snapshot.Window, "supertrend_direction")
	entryLevel, entryOK := snapshot.Indicator.Float("supertrend")
	confirmation, ok := snapshot.Timeframes["5m"]
	if !ok {
		return false, platformLevels{}
	}
	confirmationDirection := indicatorSignal(confirmation.Indicator, confirmation.Window, "supertrend_direction")
	confirmationLevel, confirmationOK := confirmation.Indicator.Float("supertrend")
	levels := platformLevels{
		entry: entryLevel, confirmation: confirmationLevel,
		entryDirection: entryDirection, confirmationDirection: confirmationDirection,
	}
	if !entryOK || !confirmationOK || entryLevel <= 0 || confirmationLevel <= 0 {
		return false, levels
	}
	if side == strategy.SignalSideBuy {
		return entryDirection == "up" && confirmationDirection == "up" && closePrice > entryLevel && closePrice > confirmationLevel, levels
	}
	return entryDirection == "down" && confirmationDirection == "down" && closePrice < entryLevel && closePrice < confirmationLevel, levels
}

func indicatorSignal(indicator strategy.IndicatorView, window strategy.IndicatorWindowView, name string) string {
	if value := indicator.Signals[name]; value != "" {
		return value
	}
	if value := indicator.Values[name]; value != "" {
		return value
	}
	if window.Schema != nil {
		if series, ok := window.Signal(name); ok {
			return series.Latest
		}
	}
	if series, ok := window.Signals[name]; ok {
		return series.Latest
	}
	return ""
}

func parsePlatformBar(snapshot strategy.Snapshot) (platformBar, error) {
	parse := func(name string, text string) (float64, error) {
		value, err := strconv.ParseFloat(text, 64)
		if err != nil || value <= 0 {
			return 0, fmt.Errorf("parse platform %s %q", name, text)
		}
		return value, nil
	}
	high, err := parse("high", snapshot.Current.High)
	if err != nil {
		return platformBar{}, err
	}
	low, err := parse("low", snapshot.Current.Low)
	if err != nil {
		return platformBar{}, err
	}
	closePrice, err := parse("close", snapshot.Current.Close)
	if err != nil {
		return platformBar{}, err
	}
	volume, err := strconv.ParseFloat(snapshot.Current.Volume, 64)
	if err != nil || volume < 0 {
		return platformBar{}, fmt.Errorf("parse platform volume %q", snapshot.Current.Volume)
	}
	if math.IsNaN(volume) || math.IsInf(volume, 0) {
		return platformBar{}, fmt.Errorf("parse platform volume %q", snapshot.Current.Volume)
	}
	openPrice, err := parse("open", snapshot.Current.Open)
	if err != nil {
		return platformBar{}, err
	}
	return platformBar{open: openPrice, high: high, low: low, close: closePrice, volume: volume}, nil
}

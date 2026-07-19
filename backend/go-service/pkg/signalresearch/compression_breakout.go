package signalresearch

import (
	"encoding/json"
	"fmt"
	"math"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/strategy"
)

const CompressionBreakoutSource = "compression_range_breakout"

type CompressionBreakoutConfig struct {
	WindowBars        int
	MinVolumeRatio    float64
	MinDirectionScore float64
	CooldownBars      int
}

func DefaultCompressionBreakoutConfig() CompressionBreakoutConfig {
	return CompressionBreakoutConfig{WindowBars: 20, MinVolumeRatio: 1.2, MinDirectionScore: 35, CooldownBars: 20}
}

type CompressionBreakoutDetector struct {
	config    CompressionBreakoutConfig
	bars      []platformBar
	lastOpen  int64
	cooldown  map[strategy.SignalSide]int
	locked    bool
	consumed  bool
	rangeHigh float64
	rangeLow  float64
}

func NewCompressionBreakoutDetector(config CompressionBreakoutConfig) (*CompressionBreakoutDetector, error) {
	if config.WindowBars < 3 || config.MinVolumeRatio <= 0 || config.MinDirectionScore <= 0 || config.MinDirectionScore > 100 || config.CooldownBars < 0 {
		return nil, fmt.Errorf("invalid compression breakout config")
	}
	return &CompressionBreakoutDetector{config: config, cooldown: map[strategy.SignalSide]int{}}, nil
}

func (d *CompressionBreakoutDetector) Update(snapshot strategy.Snapshot, regime *marketregime.Result) ([]PlatformEvent, error) {
	if snapshot.Current.OpenTime <= d.lastOpen {
		return nil, nil
	}
	bar, err := parsePlatformBar(snapshot)
	if err != nil {
		return nil, err
	}
	d.lastOpen = snapshot.Current.OpenTime
	for side, remaining := range d.cooldown {
		if remaining > 0 {
			d.cooldown[side] = remaining - 1
		}
	}
	events := make([]PlatformEvent, 0, 1)
	chopLocked := regime != nil && regime.State == marketregime.StateChopLock
	if !chopLocked {
		d.locked, d.consumed = false, false
		d.rangeHigh, d.rangeLow = 0, 0
	}
	if chopLocked && !d.locked && len(d.bars) >= d.config.WindowBars {
		window := d.bars[len(d.bars)-d.config.WindowBars:]
		d.rangeHigh, d.rangeLow = window[0].high, window[0].low
		for _, previous := range window[1:] {
			d.rangeHigh = math.Max(d.rangeHigh, previous.high)
			d.rangeLow = math.Min(d.rangeLow, previous.low)
		}
		d.locked = true
	}
	if chopLocked && d.locked && !d.consumed {
		side := strategy.SignalSideHold
		if bar.close > d.rangeHigh {
			side = strategy.SignalSideBuy
		} else if bar.close < d.rangeLow {
			side = strategy.SignalSideSell
		}
		if side != strategy.SignalSideHold && d.cooldown[side] == 0 {
			event, ok, err := d.confirm(snapshot, bar, side, d.rangeHigh, d.rangeLow, *regime)
			if err != nil {
				return nil, err
			}
			if ok {
				events = append(events, event)
				d.cooldown[side] = d.config.CooldownBars
				d.consumed = true
			}
		}
	}
	d.bars = append(d.bars, bar)
	if len(d.bars) > d.config.WindowBars {
		d.bars = append([]platformBar(nil), d.bars[len(d.bars)-d.config.WindowBars:]...)
	}
	return events, nil
}

func (d *CompressionBreakoutDetector) confirm(snapshot strategy.Snapshot, bar platformBar, side strategy.SignalSide, high float64, low float64, regime marketregime.Result) (PlatformEvent, bool, error) {
	aligned, levels := platformTrendAligned(snapshot, bar.close, side)
	if !aligned {
		return PlatformEvent{}, false, nil
	}
	if side == strategy.SignalSideBuy && regime.DirectionScore < d.config.MinDirectionScore || side == strategy.SignalSideSell && regime.DirectionScore > -d.config.MinDirectionScore {
		return PlatformEvent{}, false, nil
	}
	momentum, momentumOK := snapshot.Indicator.Float("squeeze_momentum")
	delta, deltaOK := snapshot.Indicator.Float("squeeze_momentum_delta")
	if !momentumOK || !deltaOK || side == strategy.SignalSideBuy && (momentum <= 0 || delta <= 0) || side == strategy.SignalSideSell && (momentum >= 0 || delta >= 0) {
		return PlatformEvent{}, false, nil
	}
	volumeRatio, ok := snapshot.Indicator.Float("volume_ratio20")
	if !ok || volumeRatio < d.config.MinVolumeRatio {
		return PlatformEvent{}, false, nil
	}
	metadata, err := json.Marshal(map[string]any{
		"version": "compression-range-breakout.v1", "side": side,
		"window_bars": d.config.WindowBars, "range_high": high, "range_low": low,
		"entry_close": bar.close, "volume_ratio20": volumeRatio,
		"direction_score": regime.DirectionScore, "squeeze_momentum": momentum, "squeeze_momentum_delta": delta,
		"entry_supertrend": levels.entry, "confirmation_supertrend": levels.confirmation,
	})
	if err != nil {
		return PlatformEvent{}, false, fmt.Errorf("encode compression breakout metadata: %w", err)
	}
	return PlatformEvent{Side: side, Source: CompressionBreakoutSource, MetadataJSON: string(metadata)}, true, nil
}

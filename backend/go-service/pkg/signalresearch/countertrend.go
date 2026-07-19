package signalresearch

import (
	"encoding/json"
	"fmt"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

type CounterTrendConfig struct {
	WaitBars       int
	StructureBars  int
	MinVolumeRatio float64
	SizeFactor     float64
}

type CounterTrendDecision struct {
	Allow        bool
	CounterTrend bool
	MetadataJSON string
}

type CounterTrendGate struct {
	config       CounterTrendConfig
	bars         []platformBar
	pending      platformBar
	hasPending   bool
	lastOpenTime int64
	flipAge      map[strategy.SignalSide]int
	lastRegime   string
	usedRegimes  map[string]bool
}

func NewCounterTrendGate(config CounterTrendConfig) (*CounterTrendGate, error) {
	if config.WaitBars < 0 || config.StructureBars <= 0 || config.MinVolumeRatio <= 0 || config.SizeFactor <= 0 || config.SizeFactor > 1 {
		return nil, fmt.Errorf("invalid counter-trend gate config")
	}
	return &CounterTrendGate{
		config: config,
		flipAge: map[strategy.SignalSide]int{
			strategy.SignalSideBuy:  -1,
			strategy.SignalSideSell: -1,
		},
		usedRegimes: map[string]bool{},
	}, nil
}

func (g *CounterTrendGate) Evaluate(snapshot strategy.Snapshot, side strategy.SignalSide, sources []string) (CounterTrendDecision, error) {
	if err := g.prepare(snapshot); err != nil {
		return CounterTrendDecision{}, err
	}
	if containsSource(sources, "supertrend_flip") {
		g.flipAge[side] = 0
	}
	direction15, ok15 := timeframeDirection(snapshot, "15m")
	direction30, ok30 := timeframeDirection(snapshot, "30m")
	regime := strings.Join([]string{direction15, direction30}, "|")
	if regime != g.lastRegime {
		g.lastRegime = regime
		g.usedRegimes = map[string]bool{}
	}
	wanted := directionForSide(side)
	if (ok15 && direction15 == wanted) || (ok30 && direction30 == wanted) {
		metadata, err := encodeCounterTrendMetadata(false, direction15, direction30, 1, "higher_timeframe_aligned")
		return CounterTrendDecision{Allow: true, MetadataJSON: metadata}, err
	}
	if !ok15 || !ok30 {
		return CounterTrendDecision{}, nil
	}
	age := g.flipAge[side]
	if age < g.config.WaitBars || len(g.bars) < g.config.StructureBars {
		return CounterTrendDecision{CounterTrend: true}, nil
	}
	confirmation, ok := snapshot.Timeframes["5m"]
	if !ok || indicatorSignal(confirmation.Indicator, confirmation.Window, "supertrend_direction") != wanted {
		return CounterTrendDecision{CounterTrend: true}, nil
	}
	volumeRatio, ok := snapshot.Indicator.Float("volume_ratio20")
	if !ok || volumeRatio < g.config.MinVolumeRatio {
		return CounterTrendDecision{CounterTrend: true}, nil
	}
	current, err := parsePlatformBar(snapshot)
	if err != nil {
		return CounterTrendDecision{}, err
	}
	window := g.bars[len(g.bars)-g.config.StructureBars:]
	high, low := window[0].high, window[0].low
	for _, bar := range window[1:] {
		if bar.high > high {
			high = bar.high
		}
		if bar.low < low {
			low = bar.low
		}
	}
	breakout := side == strategy.SignalSideBuy && current.close > high
	if side == strategy.SignalSideSell {
		breakout = current.close < low
	}
	if !breakout {
		return CounterTrendDecision{CounterTrend: true}, nil
	}
	regimeKey := strings.Join([]string{regime, string(side)}, "|")
	if g.usedRegimes[regimeKey] {
		return CounterTrendDecision{CounterTrend: true}, nil
	}
	g.usedRegimes[regimeKey] = true
	metadata, err := encodeCounterTrendMetadata(true, direction15, direction30, g.config.SizeFactor, "confirmed_counter_trend")
	if err != nil {
		return CounterTrendDecision{}, err
	}
	return CounterTrendDecision{Allow: true, CounterTrend: true, MetadataJSON: metadata}, nil
}

func (g *CounterTrendGate) prepare(snapshot strategy.Snapshot) error {
	if snapshot.Current.OpenTime == g.lastOpenTime {
		return nil
	}
	if snapshot.Current.OpenTime < g.lastOpenTime {
		return fmt.Errorf("counter-trend gate received out-of-order bar")
	}
	if g.hasPending {
		g.bars = append(g.bars, g.pending)
		if len(g.bars) > g.config.StructureBars {
			g.bars = append([]platformBar(nil), g.bars[len(g.bars)-g.config.StructureBars:]...)
		}
	}
	current, err := parsePlatformBar(snapshot)
	if err != nil {
		return err
	}
	g.pending = current
	g.hasPending = true
	g.lastOpenTime = snapshot.Current.OpenTime
	for side, age := range g.flipAge {
		if age >= 0 {
			g.flipAge[side] = age + 1
		}
	}
	return nil
}

func timeframeDirection(snapshot strategy.Snapshot, interval string) (string, bool) {
	view, ok := snapshot.Timeframes[interval]
	if !ok {
		return "", false
	}
	direction := indicatorSignal(view.Indicator, view.Window, "supertrend_direction")
	return direction, direction == "up" || direction == "down"
}

func directionForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "up"
	}
	return "down"
}

func containsSource(sources []string, wanted string) bool {
	for _, source := range sources {
		if source == wanted {
			return true
		}
	}
	return false
}

func encodeCounterTrendMetadata(counterTrend bool, direction15 string, direction30 string, sizeFactor float64, reason string) (string, error) {
	encoded, err := json.Marshal(map[string]any{
		"version": "counter-trend-gate.v1", "counter_trend": counterTrend,
		"direction_15m": direction15, "direction_30m": direction30,
		"size_factor": sizeFactor, "reason": reason,
	})
	if err != nil {
		return "", fmt.Errorf("encode counter-trend metadata: %w", err)
	}
	return string(encoded), nil
}

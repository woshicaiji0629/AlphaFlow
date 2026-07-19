package signalresearch

import (
	"fmt"
	"math"

	"alphaflow/go-service/pkg/strategy"
)

type ChopConfig struct {
	WindowBars          int
	MaxEfficiencyRatio  float64
	MaxADX              float64
	MinFlips            int
	MaxNormalizedSlope  float64
	MaxRangeATR         float64
	MinVotes            int
	ConfirmBars         int
	ExitBars            int
	BreakoutVolumeRatio float64
}

type ChopObservation struct {
	RunID              string
	BarCloseTimeMS     int64
	State              string
	EfficiencyRatio    float64
	SupertrendFlips10M int
	ADX10M             float64
	NormalizedSlope10M float64
	RangeATR10M        float64
	EvidenceVotes      int
	PlatformHigh       float64
	PlatformLow        float64
	FailedBreakouts    int
}

type chopTrendSample struct {
	closeTime int64
	direction string
	level     float64
	atr       float64
}

type ChopDetector struct {
	config          ChopConfig
	bars            []platformBar
	trendSamples    []chopTrendSample
	lastTrendClose  int64
	state           string
	qualifyingBars  int
	releaseBars     int
	breakoutBars    int
	breakoutSide    string
	breakoutHigh    float64
	breakoutLow     float64
	failedBreakouts int
}

func NewChopDetector(config ChopConfig) (*ChopDetector, error) {
	if config.WindowBars < 3 || config.MaxEfficiencyRatio <= 0 || config.MaxADX <= 0 || config.MinFlips <= 0 || config.MaxNormalizedSlope <= 0 || config.MaxRangeATR <= 0 || config.MinVotes <= 0 || config.MinVotes > 5 || config.ConfirmBars <= 0 || config.ExitBars <= 0 || config.BreakoutVolumeRatio <= 0 {
		return nil, fmt.Errorf("invalid chop detector config")
	}
	return &ChopDetector{config: config, state: "normal"}, nil
}

func (d *ChopDetector) Update(runID string, snapshot strategy.Snapshot) (ChopObservation, bool, error) {
	bar, err := parsePlatformBar(snapshot)
	if err != nil {
		return ChopObservation{}, false, err
	}
	d.updateTrendSamples(snapshot)
	priorHigh, priorLow, hasWindow := d.priorRange()
	d.bars = append(d.bars, bar)
	if len(d.bars) > d.config.WindowBars+1 {
		d.bars = append([]platformBar(nil), d.bars[len(d.bars)-d.config.WindowBars-1:]...)
	}
	if !hasWindow || len(d.bars) < d.config.WindowBars+1 || len(d.trendSamples) < 2 {
		return ChopObservation{}, false, nil
	}
	window := d.bars[len(d.bars)-d.config.WindowBars-1:]
	efficiency := efficiencyRatio(window)
	flips := trendFlipCount(d.trendSamples)
	latest := d.trendSamples[len(d.trendSamples)-1]
	slope := normalizedTrendSlope(d.trendSamples)
	rangeATR := 0.0
	if latest.atr > 0 {
		rangeATR = (priorHigh - priorLow) / latest.atr
	}
	adx, _ := snapshot.Timeframes["10m"].Indicator.Float("adx14")
	votes := 0
	if efficiency <= d.config.MaxEfficiencyRatio {
		votes++
	}
	if latest.atr > 0 && rangeATR <= d.config.MaxRangeATR {
		votes++
	}
	if adx > 0 && adx <= d.config.MaxADX {
		votes++
	}
	if flips >= d.config.MinFlips {
		votes++
	}
	if math.Abs(slope) <= d.config.MaxNormalizedSlope {
		votes++
	}
	d.transition(snapshot, bar.close, priorHigh, priorLow, votes)
	return ChopObservation{
		RunID: runID, BarCloseTimeMS: snapshot.Current.CloseTime, State: d.state,
		EfficiencyRatio: efficiency, SupertrendFlips10M: flips, ADX10M: adx,
		NormalizedSlope10M: slope, RangeATR10M: rangeATR, EvidenceVotes: votes,
		PlatformHigh: priorHigh, PlatformLow: priorLow, FailedBreakouts: d.failedBreakouts,
	}, true, nil
}

func (d *ChopDetector) updateTrendSamples(snapshot strategy.Snapshot) {
	view, ok := snapshot.Timeframes["10m"]
	if !ok || view.Indicator.CloseTime <= d.lastTrendClose {
		return
	}
	direction := indicatorSignal(view.Indicator, view.Window, "supertrend_direction")
	level, levelOK := view.Indicator.Float("supertrend")
	atr, atrOK := view.Indicator.Float("atr14")
	if direction == "" || !levelOK || !atrOK || atr <= 0 {
		return
	}
	d.lastTrendClose = view.Indicator.CloseTime
	d.trendSamples = append(d.trendSamples, chopTrendSample{closeTime: view.Indicator.CloseTime, direction: direction, level: level, atr: atr})
	maxSamples := d.config.WindowBars/3 + 2
	if len(d.trendSamples) > maxSamples {
		d.trendSamples = append([]chopTrendSample(nil), d.trendSamples[len(d.trendSamples)-maxSamples:]...)
	}
}

func (d *ChopDetector) priorRange() (float64, float64, bool) {
	if len(d.bars) < d.config.WindowBars {
		return 0, 0, false
	}
	window := d.bars[len(d.bars)-d.config.WindowBars:]
	high, low := window[0].high, window[0].low
	for _, bar := range window[1:] {
		high = math.Max(high, bar.high)
		low = math.Min(low, bar.low)
	}
	return high, low, true
}

func (d *ChopDetector) transition(snapshot strategy.Snapshot, closePrice float64, high float64, low float64, votes int) {
	volumeRatio, _ := snapshot.Indicator.Float("volume_ratio20")
	switch d.state {
	case "normal":
		if votes >= d.config.MinVotes {
			d.state, d.qualifyingBars = "chop_watch", 1
		}
	case "chop_watch":
		if votes >= d.config.MinVotes {
			d.qualifyingBars++
			if d.qualifyingBars >= d.config.ConfirmBars {
				d.state = "chop_confirmed"
			}
		} else if votes < d.config.MinVotes-1 {
			d.state, d.qualifyingBars = "normal", 0
		}
	case "chop_confirmed":
		if volumeRatio >= d.config.BreakoutVolumeRatio && (closePrice > high || closePrice < low) {
			d.state, d.breakoutBars = "breakout_pending", 1
			d.releaseBars = 0
			d.breakoutHigh, d.breakoutLow = high, low
			if closePrice > high {
				d.breakoutSide = "up"
			} else {
				d.breakoutSide = "down"
			}
			return
		}
		if votes < d.config.MinVotes-1 {
			d.releaseBars++
			if d.releaseBars >= d.config.ExitBars {
				d.state, d.qualifyingBars, d.releaseBars = "normal", 0, 0
			}
		} else {
			d.releaseBars = 0
		}
	case "breakout_pending":
		outside := d.breakoutSide == "up" && closePrice > d.breakoutHigh
		if d.breakoutSide == "down" {
			outside = closePrice < d.breakoutLow
		}
		if !outside {
			d.state = "chop_confirmed"
			d.releaseBars = 0
			d.failedBreakouts++
			d.breakoutBars = 0
			return
		}
		d.breakoutBars++
		if d.breakoutBars >= 2 {
			d.state, d.qualifyingBars, d.breakoutBars = "normal", 0, 0
		}
	}
}

func efficiencyRatio(bars []platformBar) float64 {
	if len(bars) < 2 {
		return 0
	}
	path := 0.0
	for index := 1; index < len(bars); index++ {
		path += math.Abs(bars[index].close - bars[index-1].close)
	}
	if path == 0 {
		return 0
	}
	return math.Abs(bars[len(bars)-1].close-bars[0].close) / path
}

func trendFlipCount(samples []chopTrendSample) int {
	flips := 0
	for index := 1; index < len(samples); index++ {
		if samples[index].direction != samples[index-1].direction {
			flips++
		}
	}
	return flips
}

func normalizedTrendSlope(samples []chopTrendSample) float64 {
	if len(samples) < 2 {
		return 0
	}
	averageATR := 0.0
	for _, sample := range samples {
		averageATR += sample.atr
	}
	averageATR /= float64(len(samples))
	if averageATR <= 0 {
		return 0
	}
	return (samples[len(samples)-1].level - samples[0].level) / averageATR / float64(len(samples)-1)
}

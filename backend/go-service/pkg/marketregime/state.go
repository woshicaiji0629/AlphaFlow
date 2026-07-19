package marketregime

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

// Version identifies a market-regime model without coupling callers to its
// concrete implementation. Versions may coexist and produce the same Result
// contract for side-by-side research.
type Version string

const (
	VersionV1 Version = "v1"
	VersionV2 Version = "v2"
	VersionV3 Version = "v3"
	VersionV4 Version = "v4"
	VersionV5 Version = "v5"
	VersionV6 Version = "v6"
)

// Analyzer is the version-neutral, stateful market-regime contract. Analyze
// must be called with closed bars in chronological order.
type Analyzer interface {
	Version() Version
	Analyze(snapshot strategy.Snapshot) (Result, bool, error)
}

type State string

const (
	StateNormal          State = "normal"
	StateChopWatch       State = "chop_watch"
	StateChopLock        State = "chop_lock"
	StateBreakoutPending State = "breakout_pending"
	StateTrendArmed      State = "trend_armed"
	StateTrendActive     State = "trend_active"
	StateFailedBreakout  State = "failed_breakout"
)

type Direction string

const (
	DirectionNone  Direction = "none"
	DirectionLong  Direction = "long"
	DirectionShort Direction = "short"
)

type Config struct {
	HigherIntervals        []string
	WindowBars             int
	MinDormantIntervals    int
	MinDormantVotes        int
	MaxMASpreadATR         float64
	MaxMACDAxisATR         float64
	MaxMACDHistogramATR    float64
	MaxMASlopePct          float64
	MaxEfficiencyRatio     float64
	MaxPlatformRangeATR    float64
	StallMaxEfficiency     float64
	StallMaxRangeATR       float64
	LockConfirmBars        int
	UnlockEvidenceBars     int
	BreakoutConfirmBars    int
	FailedBreakoutBars     int
	BreakoutVolumeRatio    float64
	MinBreakoutMACDAxisATR float64
}

func DefaultConfig() Config {
	return Config{
		HigherIntervals: []string{"15m", "30m"}, WindowBars: 60,
		MinDormantIntervals: 2, MinDormantVotes: 3,
		MaxMASpreadATR: 0.8, MaxMACDAxisATR: 0.2, MaxMACDHistogramATR: 0.08, MaxMASlopePct: 0.08,
		MaxEfficiencyRatio: 0.25, MaxPlatformRangeATR: 4,
		StallMaxEfficiency: 0.10, StallMaxRangeATR: 3.5,
		LockConfirmBars: 10, UnlockEvidenceBars: 5, BreakoutConfirmBars: 2, FailedBreakoutBars: 5,
		BreakoutVolumeRatio:    1.5,
		MinBreakoutMACDAxisATR: 0.3,
	}
}

type IntervalEvidence struct {
	Interval          string
	Available         bool
	Dormant           bool
	Votes             int
	MATangled         bool
	MASpreadATR       float64
	MACDAxisATR       float64
	MACDHistogramATR  float64
	MASlopePct        float64
	MomentumDirection Direction
}

type Result struct {
	Version           Version
	State             State
	Direction         Direction
	AllowNewPosition  bool
	AllowLong         bool
	AllowShort        bool
	TrendabilityScore float64
	DirectionScore    float64
	Confidence        float64
	Reasons           []string
	BarCloseTimeMS    int64
	DormantIntervals  int
	LockReason        string
	EfficiencyRatio   float64
	PlatformRangeATR  float64
	PlatformHigh      float64
	PlatformLow       float64
	FailedBreakouts   int
	StateBars         int
	Evidence          []IntervalEvidence
}

type priceBar struct {
	close  float64
	high   float64
	low    float64
	volume float64
}

type Detector struct {
	config             Config
	state              State
	direction          Direction
	bars               []priceBar
	stateBars          int
	lockEvidenceBars   int
	unlockEvidenceBars int
	breakoutBars       int
	failedBars         int
	breakoutHigh       float64
	breakoutLow        float64
	failedBreakouts    int
}

func NewDetector(config Config) (*Detector, error) {
	if len(config.HigherIntervals) == 0 || config.WindowBars < 3 || config.MinDormantIntervals <= 0 || config.MinDormantIntervals > len(config.HigherIntervals) || config.MinDormantVotes <= 0 || config.MinDormantVotes > 5 || config.MaxMASpreadATR <= 0 || config.MaxMACDAxisATR <= 0 || config.MaxMACDHistogramATR <= 0 || config.MaxMASlopePct <= 0 || config.MaxEfficiencyRatio <= 0 || config.MaxPlatformRangeATR <= 0 || config.StallMaxEfficiency <= 0 || config.StallMaxRangeATR <= 0 || config.LockConfirmBars <= 0 || config.UnlockEvidenceBars <= 0 || config.BreakoutConfirmBars <= 0 || config.FailedBreakoutBars <= 0 || config.BreakoutVolumeRatio <= 0 || config.MinBreakoutMACDAxisATR <= 0 {
		return nil, fmt.Errorf("invalid market regime config")
	}
	return &Detector{config: config, state: StateNormal, direction: DirectionNone}, nil
}

// NewAnalyzer constructs a requested model version behind the common
// contract. NewDetector remains available for compatibility with existing v1
// research callers.
func NewAnalyzer(version Version, config Config) (Analyzer, error) {
	switch version {
	case VersionV1:
		return NewDetector(config)
	case VersionV2:
		return NewV2Analyzer(DefaultV2Config())
	case VersionV3:
		return NewV3Analyzer(DefaultV3Config())
	case VersionV4:
		return NewV4Analyzer(DefaultV4Config())
	case VersionV5:
		return NewV5Analyzer(DefaultV5Config())
	case VersionV6:
		return NewV6Analyzer(DefaultV6Config())
	default:
		return nil, fmt.Errorf("unsupported market regime version %q", version)
	}
}

func (d *Detector) Version() Version {
	return VersionV1
}

func (d *Detector) Analyze(snapshot strategy.Snapshot) (Result, bool, error) {
	return d.Update(snapshot)
}

func (d *Detector) Update(snapshot strategy.Snapshot) (Result, bool, error) {
	bar, err := parseBar(snapshot)
	if err != nil {
		return Result{}, false, err
	}
	high, low, hasPlatform := d.platformRange()
	d.bars = append(d.bars, bar)
	if len(d.bars) > d.config.WindowBars+1 {
		d.bars = append([]priceBar(nil), d.bars[len(d.bars)-d.config.WindowBars-1:]...)
	}
	if !hasPlatform || len(d.bars) < d.config.WindowBars+1 {
		return Result{}, false, nil
	}
	evidence := make([]IntervalEvidence, 0, len(d.config.HigherIntervals))
	dormantIntervals := 0
	for _, interval := range d.config.HigherIntervals {
		item := d.intervalEvidence(interval, snapshot.Timeframes[interval])
		evidence = append(evidence, item)
		if item.Available && item.Dormant {
			dormantIntervals++
		}
	}
	referenceATR, ok := referenceATR(snapshot, d.config.HigherIntervals)
	if !ok {
		return Result{}, false, fmt.Errorf("market regime requires positive higher-timeframe atr14")
	}
	window := d.bars[len(d.bars)-d.config.WindowBars-1:]
	efficiency := efficiencyRatio(window)
	rangeATR := (high - low) / referenceATR
	higherTimeframeDormant := dormantIntervals >= d.config.MinDormantIntervals && efficiency <= d.config.MaxEfficiencyRatio && rangeATR <= d.config.MaxPlatformRangeATR
	localStall := efficiency <= d.config.StallMaxEfficiency && rangeATR <= d.config.StallMaxRangeATR
	lockReason := ""
	if higherTimeframeDormant {
		lockReason = "higher_timeframe_dormant"
	} else if localStall {
		lockReason = "local_stall"
	}
	dormant := lockReason != ""
	volumeRatio, _ := snapshot.Indicator.Float("volume_ratio20")
	d.transition(bar.close, high, low, volumeRatio, dormant, breakoutSupported(evidence, d.direction, d.config.MinBreakoutMACDAxisATR, d.config.MaxMACDHistogramATR))
	allowNewPosition, allowLong, allowShort := permissions(d.state, d.direction)
	return Result{
		Version: VersionV1, State: d.state, Direction: d.direction,
		AllowNewPosition: allowNewPosition, AllowLong: allowLong, AllowShort: allowShort,
		BarCloseTimeMS: snapshot.Current.CloseTime, DormantIntervals: dormantIntervals, LockReason: lockReason,
		EfficiencyRatio: efficiency, PlatformRangeATR: rangeATR, PlatformHigh: high, PlatformLow: low,
		FailedBreakouts: d.failedBreakouts, StateBars: d.stateBars, Evidence: evidence,
	}, true, nil
}

func permissions(state State, direction Direction) (bool, bool, bool) {
	switch state {
	case StateNormal:
		return true, true, true
	case StateTrendArmed, StateTrendActive:
		switch direction {
		case DirectionLong:
			return true, true, false
		case DirectionShort:
			return true, false, true
		}
	}
	return false, false, false
}

func referenceATR(snapshot strategy.Snapshot, intervals []string) (float64, bool) {
	for _, interval := range intervals {
		atr, ok := snapshot.Timeframes[interval].Indicator.Float("atr14")
		if ok && atr > 0 {
			return atr, true
		}
	}
	return 0, false
}

func (d *Detector) intervalEvidence(interval string, timeframe strategy.TimeframeSnapshot) IntervalEvidence {
	result := IntervalEvidence{Interval: interval}
	atr, atrOK := timeframe.Indicator.Float("atr14")
	ema25, ema25OK := timeframe.Indicator.Float("ema25")
	ema99, ema99OK := timeframe.Indicator.Float("ema99")
	macd, macdOK := timeframe.Indicator.Float("macd")
	macdSignal, signalOK := timeframe.Indicator.Float("macd_signal")
	macdHistogram, histogramOK := timeframe.Indicator.Float("macd_hist")
	slope, slopeOK := latestNumeric(timeframe.Window, "ema25_slope5_pct")
	if !atrOK || atr <= 0 || !ema25OK || !ema99OK || !macdOK || !signalOK || !histogramOK || !slopeOK {
		return result
	}
	result.Available = true
	result.MASpreadATR = math.Abs(ema25-ema99) / atr
	result.MACDAxisATR = math.Max(math.Abs(macd), math.Abs(macdSignal)) / atr
	result.MACDHistogramATR = math.Abs(macdHistogram) / atr
	result.MASlopePct = math.Abs(slope)
	switch {
	case ema25 > ema99 && macdHistogram > 0:
		result.MomentumDirection = DirectionLong
	case ema25 < ema99 && macdHistogram < 0:
		result.MomentumDirection = DirectionShort
	default:
		result.MomentumDirection = DirectionNone
	}
	phase := latestSignal(timeframe.Window, "ma_window_phase")
	tangled := latestSignal(timeframe.Window, "ma_window_tangled")
	result.MATangled = signalIs(tangled, "true", "yes", "1") || signalIs(phase, "choppy", "flat", "compressing", "tangled")
	for _, matched := range []bool{
		result.MATangled,
		result.MASpreadATR <= d.config.MaxMASpreadATR,
		result.MACDAxisATR <= d.config.MaxMACDAxisATR,
		result.MACDHistogramATR <= d.config.MaxMACDHistogramATR,
		result.MASlopePct <= d.config.MaxMASlopePct,
	} {
		if matched {
			result.Votes++
		}
	}
	result.Dormant = result.Votes >= d.config.MinDormantVotes
	return result
}

func (d *Detector) transition(closePrice float64, high float64, low float64, volumeRatio float64, dormant bool, breakoutSupport bool) {
	previous := d.state
	switch d.state {
	case StateNormal:
		if dormant {
			d.lockEvidenceBars++
			if d.lockEvidenceBars == 1 {
				d.state = StateChopWatch
			}
		}
	case StateChopWatch:
		if dormant {
			d.lockEvidenceBars++
			if d.lockEvidenceBars >= d.config.LockConfirmBars {
				d.state = StateChopLock
			}
		} else {
			d.state, d.lockEvidenceBars = StateNormal, 0
		}
	case StateChopLock:
		if volumeRatio >= d.config.BreakoutVolumeRatio && (closePrice > high || closePrice < low) {
			d.state = StateBreakoutPending
			d.breakoutHigh, d.breakoutLow = high, low
			d.breakoutBars, d.unlockEvidenceBars = 1, 0
			if closePrice > high {
				d.direction = DirectionLong
			} else {
				d.direction = DirectionShort
			}
		} else if !dormant {
			d.unlockEvidenceBars++
			if d.unlockEvidenceBars >= d.config.UnlockEvidenceBars {
				d.state, d.direction, d.lockEvidenceBars, d.unlockEvidenceBars = StateNormal, DirectionNone, 0, 0
			}
		} else {
			d.unlockEvidenceBars = 0
		}
	case StateBreakoutPending:
		if d.outsideBreakout(closePrice) {
			d.breakoutBars++
			if d.breakoutBars >= d.config.BreakoutConfirmBars && breakoutSupport {
				d.state = StateTrendArmed
			}
		} else {
			d.state, d.failedBars, d.breakoutBars = StateFailedBreakout, 1, 0
			d.failedBreakouts++
		}
	case StateTrendArmed:
		d.state = StateTrendActive
	case StateTrendActive:
		if dormant {
			d.lockEvidenceBars++
			if d.lockEvidenceBars >= d.config.LockConfirmBars {
				d.state, d.direction = StateChopLock, DirectionNone
				d.lockEvidenceBars = 0
			}
		} else {
			d.lockEvidenceBars = 0
		}
	case StateFailedBreakout:
		d.failedBars++
		if d.failedBars >= d.config.FailedBreakoutBars {
			d.state, d.direction, d.failedBars = StateChopLock, DirectionNone, 0
		}
	}
	if d.state == previous {
		d.stateBars++
	} else {
		d.stateBars = 1
	}
}

func breakoutSupported(evidence []IntervalEvidence, direction Direction, minAxisATR float64, minHistogramATR float64) bool {
	if direction == DirectionNone {
		return false
	}
	for _, item := range evidence {
		if item.Available && item.MomentumDirection == direction && item.MACDAxisATR > minAxisATR && item.MACDHistogramATR > minHistogramATR {
			return true
		}
	}
	return false
}

func (d *Detector) outsideBreakout(closePrice float64) bool {
	return d.direction == DirectionLong && closePrice > d.breakoutHigh || d.direction == DirectionShort && closePrice < d.breakoutLow
}

func (d *Detector) platformRange() (float64, float64, bool) {
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

func efficiencyRatio(bars []priceBar) float64 {
	path := 0.0
	for index := 1; index < len(bars); index++ {
		path += math.Abs(bars[index].close - bars[index-1].close)
	}
	if path == 0 {
		return 0
	}
	return math.Abs(bars[len(bars)-1].close-bars[0].close) / path
}

func parseBar(snapshot strategy.Snapshot) (priceBar, error) {
	values := []*string{&snapshot.Current.Close, &snapshot.Current.High, &snapshot.Current.Low, &snapshot.Current.Volume}
	parsed := make([]float64, len(values))
	for index, value := range values {
		result, err := strconv.ParseFloat(strings.TrimSpace(*value), 64)
		if err != nil {
			return priceBar{}, fmt.Errorf("parse market regime bar value %q: %w", *value, err)
		}
		parsed[index] = result
	}
	return priceBar{close: parsed[0], high: parsed[1], low: parsed[2], volume: parsed[3]}, nil
}

func latestNumeric(window strategy.IndicatorWindowView, name string) (float64, bool) {
	series, ok := window.Numeric(name)
	return series.Latest, ok
}

func latestSignal(window strategy.IndicatorWindowView, name string) string {
	series, _ := window.Signal(name)
	return strings.ToLower(strings.TrimSpace(series.Latest))
}

func signalIs(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.EqualFold(strings.TrimSpace(value), candidate) {
			return true
		}
	}
	return false
}

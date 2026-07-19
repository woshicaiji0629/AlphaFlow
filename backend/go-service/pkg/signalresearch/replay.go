package signalresearch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
)

const FeatureSnapshotVersion = "signal-research.v1"

type StopKind string

const (
	StopKindFixedMargin StopKind = "fixed_margin_pct"
	StopKindATR         StopKind = "atr_multiplier"
)

type Config struct {
	RunID              string
	Leverage           float64
	Horizon            time.Duration
	FixedStopMargin    []float64
	ATRStopMultipliers []float64
	TakeProfitMargin   []float64
}

type Signal struct {
	RunID               string
	SignalID            string
	Exchange            string
	Market              string
	Symbol              string
	Interval            string
	Side                strategy.SignalSide
	TriggerSources      string
	SignalTimeMS        int64
	SignalBarOpenMS     int64
	EntryPrice          float64
	ATR                 float64
	HorizonMinutes      int
	FeatureVersion      string
	FeatureSnapshotJSON string
	CreatedAtMS         int64
}

type Outcome struct {
	RunID                      string
	SignalID                   string
	StopKind                   StopKind
	StopValue                  float64
	StopDistanceBps            float64
	TakeProfitMarginPct        float64
	TakeProfitBps              float64
	Result                     string
	ExitTimeMS                 int64
	ObservedBars               int
	MaxFavorableBps            float64
	MaxAdverseBps              float64
	HighestTakeProfitMarginPct float64
	ExpiryReturnBps            float64
	CreatedAtMS                int64
}

type Replay struct {
	config   Config
	active   []*candidate
	signals  []Signal
	outcomes []Outcome
}

type candidate struct {
	signal    Signal
	expires   int64
	lastClose float64
	stops     []*stopState
}

type stopState struct {
	kind            StopKind
	value           float64
	distanceBps     float64
	observedBars    int
	maxFavorableBps float64
	maxAdverseBps   float64
	hitTimes        map[float64]int64
	hitBars         map[float64]int
}

func New(config Config) (*Replay, error) {
	if strings.TrimSpace(config.RunID) == "" {
		return nil, fmt.Errorf("run id is required")
	}
	if config.Leverage <= 0 {
		return nil, fmt.Errorf("leverage must be positive")
	}
	if config.Horizon <= 0 {
		return nil, fmt.Errorf("horizon must be positive")
	}
	if len(config.FixedStopMargin) == 0 && len(config.ATRStopMultipliers) == 0 {
		return nil, fmt.Errorf("at least one stop is required")
	}
	if len(config.TakeProfitMargin) == 0 {
		return nil, fmt.Errorf("at least one take profit is required")
	}
	config.FixedStopMargin = normalizedPositive(config.FixedStopMargin)
	config.ATRStopMultipliers = normalizedPositive(config.ATRStopMultipliers)
	config.TakeProfitMargin = normalizedPositive(config.TakeProfitMargin)
	return &Replay{config: config}, nil
}

func (r *Replay) AddSignal(snapshot strategy.Snapshot, side strategy.SignalSide, sources []string) error {
	entryPrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil || entryPrice <= 0 {
		return fmt.Errorf("parse signal entry price %q", snapshot.Current.Close)
	}
	atr, _ := snapshot.Indicator.Float("atr14")
	if atr <= 0 {
		if series, ok := snapshot.Window.Numeric("atr14"); ok {
			atr = series.Latest
		}
	}
	featureJSON, err := encodeFeatureSnapshot(snapshot)
	if err != nil {
		return err
	}
	sourceText := strings.Join(sources, ",")
	signalTime := snapshot.Current.CloseTime
	signal := Signal{
		RunID: r.config.RunID, SignalID: signalID(r.config.RunID, snapshot, side, sourceText),
		Exchange: snapshot.Target.Exchange, Market: snapshot.Target.Market, Symbol: snapshot.Target.Symbol,
		Interval: snapshot.Target.Interval, Side: side, TriggerSources: sourceText,
		SignalTimeMS: signalTime, SignalBarOpenMS: snapshot.Current.OpenTime, EntryPrice: entryPrice, ATR: atr,
		HorizonMinutes: int(r.config.Horizon / time.Minute), FeatureVersion: FeatureSnapshotVersion,
		FeatureSnapshotJSON: featureJSON, CreatedAtMS: time.Now().UnixMilli(),
	}
	item := &candidate{signal: signal, expires: signalTime + r.config.Horizon.Milliseconds(), lastClose: entryPrice}
	for _, value := range r.config.FixedStopMargin {
		item.stops = append(item.stops, &stopState{kind: StopKindFixedMargin, value: value, distanceBps: marginPctToBps(value, r.config.Leverage), hitTimes: map[float64]int64{}, hitBars: map[float64]int{}})
	}
	if atr > 0 {
		atrBps := atr / entryPrice * 10000
		for _, value := range r.config.ATRStopMultipliers {
			item.stops = append(item.stops, &stopState{kind: StopKindATR, value: value, distanceBps: atrBps * value, hitTimes: map[float64]int64{}, hitBars: map[float64]int{}})
		}
	}
	if len(item.stops) == 0 {
		return fmt.Errorf("signal %s has no usable stop model", signal.SignalID)
	}
	r.signals = append(r.signals, signal)
	r.active = append(r.active, item)
	return nil
}

// Advance must be called before adding signals from the same bar, so a signal
// starts observing from the following kline.
func (r *Replay) Advance(kline marketmodel.Kline) error {
	high, err := strconv.ParseFloat(kline.High, 64)
	if err != nil || high <= 0 {
		return fmt.Errorf("parse kline high %q", kline.High)
	}
	low, err := strconv.ParseFloat(kline.Low, 64)
	if err != nil || low <= 0 {
		return fmt.Errorf("parse kline low %q", kline.Low)
	}
	closePrice, err := strconv.ParseFloat(kline.Close, 64)
	if err != nil || closePrice <= 0 {
		return fmt.Errorf("parse kline close %q", kline.Close)
	}
	remaining := r.active[:0]
	for _, item := range r.active {
		if kline.CloseTime > item.expires {
			r.finalizeCandidate(item, "expired", item.expires)
			continue
		}
		item.lastClose = closePrice
		liveStops := item.stops[:0]
		for _, state := range item.stops {
			state.observedBars++
			favorable, adverse := excursionBps(item.signal.Side, item.signal.EntryPrice, high, low)
			state.maxFavorableBps = math.Max(state.maxFavorableBps, favorable)
			state.maxAdverseBps = math.Max(state.maxAdverseBps, adverse)
			if adverse >= state.distanceBps {
				r.finalizeStop(item, state, "stop_loss", kline.CloseTime)
				continue
			}
			for _, marginPct := range r.config.TakeProfitMargin {
				if _, exists := state.hitTimes[marginPct]; !exists && favorable >= marginPctToBps(marginPct, r.config.Leverage) {
					state.hitTimes[marginPct] = kline.CloseTime
					state.hitBars[marginPct] = state.observedBars
				}
			}
			liveStops = append(liveStops, state)
		}
		item.stops = liveStops
		if len(item.stops) > 0 {
			remaining = append(remaining, item)
		}
	}
	r.active = remaining
	return nil
}

func (r *Replay) Finish() {
	for _, item := range r.active {
		r.finalizeCandidate(item, "expired", item.expires)
	}
	r.active = nil
}

func (r *Replay) Results() ([]Signal, []Outcome) {
	return append([]Signal(nil), r.signals...), append([]Outcome(nil), r.outcomes...)
}

func (r *Replay) finalizeCandidate(item *candidate, result string, at int64) {
	for _, state := range item.stops {
		r.finalizeStop(item, state, result, at)
	}
}

func (r *Replay) finalizeStop(item *candidate, state *stopState, fallback string, at int64) {
	highest := 0.0
	for tier := range state.hitTimes {
		highest = math.Max(highest, tier)
	}
	expiryReturn := directionalReturnBps(item.signal.Side, item.signal.EntryPrice, item.lastClose)
	for _, tier := range r.config.TakeProfitMargin {
		result := fallback
		exitTime := at
		observedBars := state.observedBars
		if hitTime, ok := state.hitTimes[tier]; ok {
			result = "take_profit"
			exitTime = hitTime
			observedBars = state.hitBars[tier]
		}
		r.outcomes = append(r.outcomes, Outcome{
			RunID: item.signal.RunID, SignalID: item.signal.SignalID, StopKind: state.kind,
			StopValue: state.value, StopDistanceBps: state.distanceBps,
			TakeProfitMarginPct: tier, TakeProfitBps: marginPctToBps(tier, r.config.Leverage),
			Result: result, ExitTimeMS: exitTime, ObservedBars: observedBars,
			MaxFavorableBps: state.maxFavorableBps, MaxAdverseBps: state.maxAdverseBps,
			HighestTakeProfitMarginPct: highest, ExpiryReturnBps: expiryReturn, CreatedAtMS: time.Now().UnixMilli(),
		})
	}
}

func encodeFeatureSnapshot(snapshot strategy.Snapshot) (string, error) {
	timeframes := make(map[string]any, len(snapshot.Timeframes))
	for interval, timeframe := range snapshot.Timeframes {
		timeframes[interval] = map[string]any{
			"indicator":      timeframe.Indicator,
			"window_numeric": timeframe.Window.AllNumeric(),
			"window_signals": timeframe.Window.AllSignals(),
		}
	}
	payload := map[string]any{
		"version":        FeatureSnapshotVersion,
		"as_of":          snapshot.AsOf,
		"current":        snapshot.Current,
		"indicator":      snapshot.Indicator,
		"window_numeric": snapshot.Window.AllNumeric(),
		"window_signals": snapshot.Window.AllSignals(),
		"timeframes":     timeframes,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode signal feature snapshot: %w", err)
	}
	return string(encoded), nil
}

func signalID(runID string, snapshot strategy.Snapshot, side strategy.SignalSide, sources string) string {
	value := fmt.Sprintf("%s|%s|%s|%s|%d|%s|%s", runID, snapshot.Target.Exchange, snapshot.Target.Symbol, snapshot.Target.Interval, snapshot.Current.CloseTime, side, sources)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func normalizedPositive(values []float64) []float64 {
	result := make([]float64, 0, len(values))
	seen := map[float64]struct{}{}
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Float64s(result)
	return result
}

func marginPctToBps(value float64, leverage float64) float64 {
	return value / leverage * 100
}

func excursionBps(side strategy.SignalSide, entry float64, high float64, low float64) (float64, float64) {
	if side == strategy.SignalSideSell {
		return math.Max(0, (entry-low)/entry*10000), math.Max(0, (high-entry)/entry*10000)
	}
	return math.Max(0, (high-entry)/entry*10000), math.Max(0, (entry-low)/entry*10000)
}

func directionalReturnBps(side strategy.SignalSide, entry float64, exit float64) float64 {
	value := (exit - entry) / entry * 10000
	if side == strategy.SignalSideSell {
		return -value
	}
	return value
}

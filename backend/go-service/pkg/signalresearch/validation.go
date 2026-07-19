package signalresearch

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"alphaflow/go-service/pkg/strategy"
)

type ValidationConfig struct {
	ObservationBars []int
}

type ValidationObservation struct {
	RunID               string
	SignalID            string
	ObservationBars     int
	ObservationTimeMS   int64
	MaxFavorableBps     float64
	MaxAdverseBps       float64
	CloseReturnBps      float64
	SignalStructureHeld bool
	Confirmation5M      string
	CreatedAtMS         int64
}

type ValidationReplay struct {
	config       ValidationConfig
	active       []*validationCandidate
	observations []ValidationObservation
}

type validationCandidate struct {
	runID         string
	signalID      string
	side          strategy.SignalSide
	entryPrice    float64
	structureHigh float64
	structureLow  float64
	observedBars  int
	maxFavorable  float64
	maxAdverse    float64
	structureHeld bool
}

func NewValidationReplay(config ValidationConfig) (*ValidationReplay, error) {
	if len(config.ObservationBars) == 0 {
		return nil, fmt.Errorf("validation observation bars are required")
	}
	sort.Ints(config.ObservationBars)
	unique := config.ObservationBars[:0]
	for _, bars := range config.ObservationBars {
		if bars <= 0 {
			return nil, fmt.Errorf("validation observation bars must be positive")
		}
		if len(unique) == 0 || unique[len(unique)-1] != bars {
			unique = append(unique, bars)
		}
	}
	config.ObservationBars = unique
	return &ValidationReplay{config: config}, nil
}

func (r *ValidationReplay) AddSignal(runID string, snapshot strategy.Snapshot, side strategy.SignalSide, sources []string) error {
	entryPrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil || entryPrice <= 0 {
		return fmt.Errorf("parse validation entry price %q", snapshot.Current.Close)
	}
	high, err := strconv.ParseFloat(snapshot.Current.High, 64)
	if err != nil || high <= 0 {
		return fmt.Errorf("parse validation structure high %q", snapshot.Current.High)
	}
	low, err := strconv.ParseFloat(snapshot.Current.Low, 64)
	if err != nil || low <= 0 {
		return fmt.Errorf("parse validation structure low %q", snapshot.Current.Low)
	}
	r.active = append(r.active, &validationCandidate{
		runID: runID, signalID: signalID(runID, snapshot, side, joinSources(sources)), side: side,
		entryPrice: entryPrice, structureHigh: high, structureLow: low, structureHeld: true,
	})
	return nil
}

func (r *ValidationReplay) Advance(snapshot strategy.Snapshot) error {
	high, err := strconv.ParseFloat(snapshot.Current.High, 64)
	if err != nil || high <= 0 {
		return fmt.Errorf("parse validation high %q", snapshot.Current.High)
	}
	low, err := strconv.ParseFloat(snapshot.Current.Low, 64)
	if err != nil || low <= 0 {
		return fmt.Errorf("parse validation low %q", snapshot.Current.Low)
	}
	closePrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil || closePrice <= 0 {
		return fmt.Errorf("parse validation close %q", snapshot.Current.Close)
	}
	confirmation := ""
	if view, ok := snapshot.Timeframes["5m"]; ok {
		confirmation = indicatorSignal(view.Indicator, view.Window, "supertrend_direction")
	}
	maxBars := r.config.ObservationBars[len(r.config.ObservationBars)-1]
	remaining := r.active[:0]
	for _, item := range r.active {
		item.observedBars++
		favorable, adverse := excursionBps(item.side, item.entryPrice, high, low)
		item.maxFavorable = max(item.maxFavorable, favorable)
		item.maxAdverse = max(item.maxAdverse, adverse)
		if item.side == strategy.SignalSideBuy && closePrice < item.structureLow {
			item.structureHeld = false
		}
		if item.side == strategy.SignalSideSell && closePrice > item.structureHigh {
			item.structureHeld = false
		}
		if containsInt(r.config.ObservationBars, item.observedBars) {
			r.observations = append(r.observations, ValidationObservation{
				RunID: item.runID, SignalID: item.signalID, ObservationBars: item.observedBars,
				ObservationTimeMS: snapshot.Current.CloseTime, MaxFavorableBps: item.maxFavorable,
				MaxAdverseBps: item.maxAdverse, CloseReturnBps: directionalReturnBps(item.side, item.entryPrice, closePrice),
				SignalStructureHeld: item.structureHeld, Confirmation5M: confirmation, CreatedAtMS: time.Now().UnixMilli(),
			})
		}
		if item.observedBars < maxBars {
			remaining = append(remaining, item)
		}
	}
	r.active = remaining
	return nil
}

func (r *ValidationReplay) Results() []ValidationObservation {
	return append([]ValidationObservation(nil), r.observations...)
}

func joinSources(sources []string) string {
	result := ""
	for index, source := range sources {
		if index > 0 {
			result += ","
		}
		result += source
	}
	return result
}

func containsInt(values []int, wanted int) bool {
	index := sort.SearchInts(values, wanted)
	return index < len(values) && values[index] == wanted
}

package strategy

import (
	"strconv"
	"sync"

	"alphaflow/go-service/pkg/marketmodel"
)

type SignalSide string

type TriggerMode string

const (
	TriggerOnEntryClose TriggerMode = "entry_close"
)

type Requirements struct {
	EntryInterval    string
	ConfirmIntervals []string
	Trigger          TriggerMode
}

const (
	SignalSideBuy  SignalSide = "buy"
	SignalSideSell SignalSide = "sell"
	SignalSideHold SignalSide = "hold"
)

type PositionSide string

const (
	PositionSideLong  PositionSide = "long"
	PositionSideShort PositionSide = "short"
	PositionSideFlat  PositionSide = "flat"
)

type PositionScope string

const (
	PositionScopeBacktest PositionScope = "bt"
	PositionScopePaper    PositionScope = "paper"
	PositionScopeTestnet  PositionScope = "testnet"
	PositionScopeLive     PositionScope = "live"
)

type ExchangePositionMode string

const (
	ExchangePositionModeNet   ExchangePositionMode = "net"
	ExchangePositionModeHedge ExchangePositionMode = "hedge"
)

type ExchangePositionSide string

const (
	ExchangePositionSideNet   ExchangePositionSide = "net"
	ExchangePositionSideLong  ExchangePositionSide = "long"
	ExchangePositionSideShort ExchangePositionSide = "short"
)

type PositionAction string

const (
	PositionActionOpenLong    PositionAction = "open_long"
	PositionActionOpenShort   PositionAction = "open_short"
	PositionActionCloseLong   PositionAction = "close_long"
	PositionActionCloseShort  PositionAction = "close_short"
	PositionActionReduceLong  PositionAction = "reduce_long"
	PositionActionReduceShort PositionAction = "reduce_short"
	PositionActionHold        PositionAction = "hold"
)

type ExitReasonType string

const (
	ExitReasonStrategy     ExitReasonType = "strategy"
	ExitReasonTakeProfit   ExitReasonType = "take_profit"
	ExitReasonStopLoss     ExitReasonType = "stop_loss"
	ExitReasonTrailingStop ExitReasonType = "trailing_stop"
	ExitReasonPartialExit  ExitReasonType = "partial_exit"
)

type Target struct {
	Exchange string
	Market   string
	Symbol   string
	Interval string
	Account  string
	Scope    PositionScope
	RunID    string
}

type Snapshot struct {
	Target     Target
	Current    marketmodel.Kline
	Indicator  IndicatorView
	Window     IndicatorWindowView
	Timeframes map[string]TimeframeSnapshot
	Price      PriceView
	Health     HealthView
	Realtime   *RealtimeView
	AsOf       int64
	Trigger    TriggerMode
	UpdatedAt  int64
}

type RealtimeView struct {
	Current   marketmodel.Kline
	Indicator IndicatorView
	Price     PriceView
}

type TimeframeSnapshot struct {
	Interval  string
	Indicator IndicatorView
	Window    IndicatorWindowView
	Health    HealthView
	UpdatedAt int64
}

type IndicatorView struct {
	OpenTime      int64
	CloseTime     int64
	Values        map[string]string
	NumericValues map[string]float64
	Signals       map[string]string
	UpdatedAt     int64
}

func (v IndicatorView) Float(name string) (float64, bool) {
	if value, ok := v.NumericValues[name]; ok {
		return value, true
	}
	text, ok := v.Values[name]
	if !ok || text == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(text, 64)
	return value, err == nil
}

type IndicatorWindowView struct {
	OpenTime        int64
	CloseTime       int64
	Version         string
	SampleCount     int
	Values          map[string]NumericSeries
	Signals         map[string]SignalSeries
	Schema          *IndicatorWindowSchema
	DenseValues     []DenseNumericSeries
	DenseDirections []string
	DenseSignals    []DenseSignalSeries
	NumericPresent  []uint64
	SignalPresent   []uint64
	UpdatedAt       int64
}

// IndicatorWindowSchema assigns append-only indexes to window fields. A
// builder may discover fields while previously built views are being read.
type IndicatorWindowSchema struct {
	mu             sync.RWMutex
	numericIndexes map[string]int
	signalIndexes  map[string]int
}

func NewIndicatorWindowSchema() *IndicatorWindowSchema {
	return &IndicatorWindowSchema{
		numericIndexes: map[string]int{},
		signalIndexes:  map[string]int{},
	}
}

func (s *IndicatorWindowSchema) EnsureNumeric(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index, ok := s.numericIndexes[name]; ok {
		return index
	}
	index := len(s.numericIndexes)
	s.numericIndexes[name] = index
	return index
}

func (s *IndicatorWindowSchema) EnsureSignal(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index, ok := s.signalIndexes[name]; ok {
		return index
	}
	index := len(s.signalIndexes)
	s.signalIndexes[name] = index
	return index
}

func (s *IndicatorWindowSchema) NumericIndex(name string) (int, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, ok := s.numericIndexes[name]
	return index, ok
}

func (s *IndicatorWindowSchema) SignalIndex(name string) (int, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, ok := s.signalIndexes[name]
	return index, ok
}

func (v IndicatorWindowView) Numeric(name string) (NumericSeries, bool) {
	if index, ok := v.Schema.NumericIndex(name); ok && index < len(v.DenseValues) && densePresent(v.NumericPresent, index) {
		value := v.DenseValues[index]
		direction := ""
		if index < len(v.DenseDirections) {
			direction = v.DenseDirections[index]
		}
		return NumericSeries{
			Latest: value.Latest, Previous: value.Previous, Change: value.Change,
			ChangePct: value.ChangePct, Slope: value.Slope, Direction: direction,
			RisingCount: value.RisingCount, FallingCount: value.FallingCount,
			Minimum: value.Minimum, Maximum: value.Maximum,
			RangePositionPct: value.RangePositionPct,
		}, true
	}
	value, ok := v.Values[name]
	return value, ok
}

func (v IndicatorWindowView) Signal(name string) (SignalSeries, bool) {
	if index, ok := v.Schema.SignalIndex(name); ok && index < len(v.DenseSignals) && densePresent(v.SignalPresent, index) {
		return v.DenseSignals[index], true
	}
	value, ok := v.Signals[name]
	return value, ok
}

func (v IndicatorWindowView) Empty() bool {
	return len(v.Values) == 0 && len(v.Signals) == 0 && !denseAny(v.NumericPresent) && !denseAny(v.SignalPresent)
}

func densePresent(words []uint64, index int) bool {
	word := index / 64
	return word < len(words) && words[word]&(uint64(1)<<uint(index%64)) != 0
}

func denseAny(words []uint64) bool {
	for _, word := range words {
		if word != 0 {
			return true
		}
	}
	return false
}

type NumericSeries struct {
	Latest           float64
	Previous         float64
	Change           float64
	ChangePct        float64
	Slope            float64
	Direction        string
	RisingCount      int
	FallingCount     int
	Minimum          float64
	Maximum          float64
	RangePositionPct float64
}

// DenseNumericSeries is the compact in-memory representation used by replay
// window views.
type DenseNumericSeries struct {
	Latest           float64
	Previous         float64
	Change           float64
	ChangePct        float64
	Slope            float64
	Minimum          float64
	Maximum          float64
	RangePositionPct float64
	RisingCount      int
	FallingCount     int
}

type SignalSeries struct {
	Latest         string
	Previous       string
	Changed        bool
	StableCount    int
	LastChangedAgo int
}

// DenseSignalSeries keeps the dense view API explicit while retaining the
// complete signal representation without a conversion copy.
type DenseSignalSeries = SignalSeries

type PriceView struct {
	LastPrice string
	MarkPrice string
}

type HealthView struct {
	OK        bool
	Reason    string
	UpdatedAt int64
}

type Signal struct {
	Exchange   string
	Market     string
	Symbol     string
	Interval   string
	Strategy   string
	Side       SignalSide
	Score      float64
	Confidence float64
	Reason     string
	OpenTime   int64
	UpdatedAt  int64
}

type Result struct {
	StrategyName string
	Signal       Signal
	Analysis     Analysis
	ExitRules    []ExitRule
}

type Analysis struct {
	Summary    string
	Trend      string
	Momentum   string
	Volatility string
	Volume     string
	Risk       string
	KeyLevels  map[string]string
	Checks     []DiagnosticCheck
}

type DiagnosticStatus string

const (
	DiagnosticStatusPass    DiagnosticStatus = "pass"
	DiagnosticStatusBlocked DiagnosticStatus = "blocked"
	DiagnosticStatusMissing DiagnosticStatus = "missing"
	DiagnosticStatusInfo    DiagnosticStatus = "info"
)

type DiagnosticCheck struct {
	Name   string
	Side   SignalSide
	Status DiagnosticStatus
	Score  float64
	Reason string
	Values map[string]string
}

type ExitRule struct {
	Type         ExitReasonType
	Reason       string
	TriggerPrice string
	SizePct      float64
	Metadata     map[string]string
}

type Position struct {
	Scope        PositionScope
	RunID        string
	Exchange     string
	Market       string
	Symbol       string
	Account      string
	StrategyName string
	PositionID   string
	Mode         ExchangePositionMode
	PositionSide ExchangePositionSide
	Side         PositionSide
	Size         float64
	InitialSize  float64
	EntryPrice   string
	HighestPrice string
	LowestPrice  string
	ExitRules    []ExitRule
	EntryTime    int64
	EntryReason  string
	UpdatedAt    int64
}

func (p Position) IsFlat() bool {
	return p.Side == PositionSideFlat || p.Size <= 0
}

type OrderPlan struct {
	Action        PositionAction
	TargetSide    PositionSide
	TargetSize    float64
	Reason        string
	ExitRules     []ExitRule
	ExitSize      float64
	ExitReason    ExitReasonType
	TriggeredRule *ExitRule
}

type Context struct {
	Target    Target
	Snapshots map[string]Snapshot
	Positions map[string]*Position
}

type Decision struct {
	Target   Target
	Results  []Result
	Failures []StrategyFailure
}

type StrategyFailure struct {
	StrategyName   string
	Error          string
	DurationMillis int64
}

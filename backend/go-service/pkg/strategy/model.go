package strategy

import "alphaflow/go-service/pkg/marketmodel"

type SignalSide string

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
	UpdatedAt  int64
}

type TimeframeSnapshot struct {
	Interval  string
	Indicator IndicatorView
	Window    IndicatorWindowView
	Health    HealthView
	UpdatedAt int64
}

type IndicatorView struct {
	OpenTime  int64
	CloseTime int64
	Values    map[string]string
	Signals   map[string]string
	UpdatedAt int64
}

type IndicatorWindowView struct {
	OpenTime    int64
	CloseTime   int64
	Version     string
	SampleCount int
	Values      map[string]NumericSeries
	Signals     map[string]SignalSeries
	UpdatedAt   int64
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

type SignalSeries struct {
	Latest         string
	Previous       string
	Changed        bool
	StableCount    int
	LastChangedAgo int
}

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
	Target  Target
	Results []Result
}

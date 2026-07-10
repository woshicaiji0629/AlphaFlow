package execution

import "alphaflow/go-service/pkg/strategy"

type OrderAction string

const (
	OrderActionOpen    OrderAction = "open"
	OrderActionClose   OrderAction = "close"
	OrderActionReduce  OrderAction = "reduce"
	OrderActionReverse OrderAction = "reverse"
)

type OrderSide string

const (
	OrderSideBuy  OrderSide = "buy"
	OrderSideSell OrderSide = "sell"
)

type OrderType string

const (
	OrderTypeMarket OrderType = "market"
	OrderTypeLimit  OrderType = "limit"
	OrderTypeStop   OrderType = "stop"
)

type ExecutionStatus string

const (
	ExecutionStatusAccepted ExecutionStatus = "accepted"
	ExecutionStatusRejected ExecutionStatus = "rejected"
	ExecutionStatusPartial  ExecutionStatus = "partial"
	ExecutionStatusFilled   ExecutionStatus = "filled"
	ExecutionStatusCanceled ExecutionStatus = "canceled"
)

type OrderIntent struct {
	IntentID       string
	IdempotencyKey string
	StrategyName   string
	Scope          string
	Exchange       string
	Account        string
	RunID          string
	Market         string
	Symbol         string
	PositionSide   string
	Action         OrderAction
	Side           OrderSide
	Type           OrderType
	Quantity       float64
	ReferencePrice string
	LimitPrice     string
	StopPrice      string
	ReduceOnly     bool
	Reason         string
	BarOpenTime    int64
	ExitRules      []strategy.ExitRule
	TriggeredRule  *strategy.ExitRule
	CreatedAt      int64
}

type ExecutionReport struct {
	IntentID        string
	ExchangeOrderID string
	Status          ExecutionStatus
	FilledQuantity  float64
	AveragePrice    string
	Fee             float64
	Error           string
	UpdatedAt       int64
}

type IntentState string

const (
	IntentStateCreated         IntentState = "created"
	IntentStateSubmitted       IntentState = "submitted"
	IntentStateFilled          IntentState = "filled"
	IntentStatePositionApplied IntentState = "position_applied"
	IntentStateCompleted       IntentState = "completed"
	IntentStateRejected        IntentState = "rejected"
)

type IntentRecord struct {
	Intent    OrderIntent
	Report    ExecutionReport
	State     IntentState
	UpdatedAt int64
}

type AccountSnapshot struct {
	Scope            string
	Account          string
	Exchange         string
	Market           string
	Equity           string
	AvailableBalance string
	UsedMargin       string
	UnrealizedPnL    string
	UpdatedAt        int64
}

type SymbolCapability struct {
	Exchange     string
	Market       string
	Symbol       string
	MinQty       string
	QtyStep      string
	MinNotional  string
	MaxLeverage  string
	MaxOrderQty  string
	ContractSize string
	UpdatedAt    int64
}

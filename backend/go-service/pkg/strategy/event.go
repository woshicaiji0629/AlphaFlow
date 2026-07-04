package strategy

type EventType string

const (
	EventTypeSignalGenerated        EventType = "signal_generated"
	EventTypePositionOpened         EventType = "position_opened"
	EventTypePositionUpdated        EventType = "position_updated"
	EventTypeExitRuleUpdated        EventType = "exit_rule_updated"
	EventTypePositionReduced        EventType = "position_reduced"
	EventTypePositionClosed         EventType = "position_closed"
	EventTypeOrderIntentCreated     EventType = "order_intent_created"
	EventTypeOrderFilled            EventType = "order_filled"
	EventTypeAccountSnapshot        EventType = "account_snapshot"
	EventTypeExchangePositionSynced EventType = "exchange_position_synced"
)

type StrategyEvent struct {
	EventID         string
	Scope           PositionScope
	RunID           string
	Account         string
	Exchange        string
	Market          string
	Symbol          string
	StrategyName    string
	EventType       EventType
	EventTime       int64
	BarOpenTime     int64
	Side            SignalSide
	PositionSide    ExchangePositionSide
	PositionMode    ExchangePositionMode
	Size            float64
	Price           string
	Notional        string
	Fee             string
	PnL             string
	Reason          string
	Score           float64
	Confidence      float64
	OrderID         string
	IntentID        string
	ExchangeOrderID string
	Metadata        map[string]string
	CreatedAt       int64
}

type BacktestRunStatus string

const (
	BacktestRunStatusRunning   BacktestRunStatus = "running"
	BacktestRunStatusCompleted BacktestRunStatus = "completed"
	BacktestRunStatusFailed    BacktestRunStatus = "failed"
)

type BacktestRunSummary struct {
	RunID         string
	Status        BacktestRunStatus
	StrategySet   string
	Exchange      string
	Market        string
	Symbols       []string
	StartTime     int64
	EndTime       int64
	TotalTrades   int64
	WinRate       float64
	NetPnL        string
	MaxDrawdown   string
	ProfitFactor  float64
	Sharpe        float64
	FailureReason string
	Metadata      map[string]string
	CreatedAt     int64
	UpdatedAt     int64
}

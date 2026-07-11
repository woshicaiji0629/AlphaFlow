package domain

import "time"

type TradingAccount struct{ Mode, AccountKey, DisplayName string }

type Dashboard struct {
	AsOf       time.Time           `json:"asOf"`
	Mode       string              `json:"mode"`
	Metrics    []DashboardMetric   `json:"metrics"`
	Services   []ServiceHealth     `json:"services"`
	Positions  []DashboardPosition `json:"positions"`
	Signals    []DashboardSignal   `json:"signals"`
	Equity     []EquityPoint       `json:"equity"`
	DataStatus map[string]string   `json:"dataStatus"`
}
type DashboardMetric struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Change string `json:"change,omitempty"`
	Trend  string `json:"trend,omitempty"`
}
type ServiceHealth struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Status string `json:"status"`
}
type DashboardPosition struct {
	ID         string   `json:"id"`
	Symbol     string   `json:"symbol"`
	Strategy   string   `json:"strategy"`
	Side       string   `json:"side"`
	Account    string   `json:"account"`
	Scope      string   `json:"scope"`
	Leverage   float64  `json:"leverage"`
	EntryPrice float64  `json:"entryPrice"`
	MarkPrice  *float64 `json:"markPrice"`
	PnL        *float64 `json:"pnl"`
	PnLPercent *float64 `json:"pnlPercent"`
}
type DashboardSignal struct {
	ID         string  `json:"id"`
	Time       string  `json:"time"`
	Symbol     string  `json:"symbol"`
	Strategy   string  `json:"strategy"`
	Signal     string  `json:"signal"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}
type EquityPoint struct {
	Time  string  `json:"time"`
	Value float64 `json:"value"`
}

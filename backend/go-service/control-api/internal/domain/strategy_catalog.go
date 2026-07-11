package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrStrategyNotFound = errors.New("strategy not found")
var ErrStrategyNotEditable = errors.New("strategy is not editable")

type AdminStrategy struct {
	ID           uuid.UUID         `json:"id"`
	Code         string            `json:"code"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Version      string            `json:"version"`
	Parameters   map[string]string `json:"parameters"`
	Status       string            `json:"status"`
	Visibility   string            `json:"visibility"`
	RiskLevel    string            `json:"riskLevel"`
	PaperEnabled bool              `json:"paperEnabled"`
	LiveEnabled  bool              `json:"liveEnabled"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

type PublishedStrategy struct {
	ID           uuid.UUID `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Version      string    `json:"version"`
	RiskLevel    string    `json:"riskLevel"`
	PaperEnabled bool      `json:"paperEnabled"`
	LiveEnabled  bool      `json:"liveEnabled"`
}
type StrategyPerformance struct {
	ID              uuid.UUID       `json:"id"`
	StrategyID      uuid.UUID       `json:"strategyId"`
	StrategyVersion string          `json:"strategyVersion"`
	Exchange        string          `json:"exchange"`
	Market          string          `json:"market"`
	SymbolScope     json.RawMessage `json:"symbolScope"`
	Interval        string          `json:"interval"`
	PeriodStart     time.Time       `json:"periodStart"`
	PeriodEnd       time.Time       `json:"periodEnd"`
	Metrics         json.RawMessage `json:"metrics"`
	EquityPoints    json.RawMessage `json:"equityPoints"`
	PublishedAt     time.Time       `json:"publishedAt"`
}

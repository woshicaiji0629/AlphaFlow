package simulator

import (
	"strconv"

	"alphaflow/go-service/pkg/strategy"
)

type SummaryOptions struct {
	RunID       string
	StrategySet string
	Exchange    string
	Market      string
	Symbols     []string
	StartTime   int64
	EndTime     int64
	CreatedAt   int64
	UpdatedAt   int64
}

func BuildBacktestRunSummary(events []strategy.StrategyEvent, options SummaryOptions) strategy.BacktestRunSummary {
	totalPnL := 0.0
	exitTrades := int64(0)
	wins := int64(0)
	grossProfit := 0.0
	grossLoss := 0.0
	for _, event := range events {
		if event.EventType != strategy.EventTypeOrderFilled {
			continue
		}
		pnl, ok := parseEventFloat(event.PnL)
		if ok {
			totalPnL += pnl
		}
		if !isExitFill(event) || !ok {
			continue
		}
		exitTrades++
		if pnl > 0 {
			wins++
			grossProfit += pnl
		}
		if pnl < 0 {
			grossLoss += -pnl
		}
	}
	winRate := 0.0
	if exitTrades > 0 {
		winRate = float64(wins) / float64(exitTrades)
	}
	profitFactor := 0.0
	if grossLoss > 0 {
		profitFactor = grossProfit / grossLoss
	}
	updatedAt := options.UpdatedAt
	if updatedAt == 0 {
		updatedAt = options.EndTime
	}
	createdAt := options.CreatedAt
	if createdAt == 0 {
		createdAt = options.StartTime
	}
	return strategy.BacktestRunSummary{
		RunID:        options.RunID,
		Status:       strategy.BacktestRunStatusCompleted,
		StrategySet:  options.StrategySet,
		Exchange:     options.Exchange,
		Market:       options.Market,
		Symbols:      append([]string(nil), options.Symbols...),
		StartTime:    options.StartTime,
		EndTime:      options.EndTime,
		TotalTrades:  exitTrades,
		WinRate:      winRate,
		NetPnL:       formatSummaryFloat(totalPnL),
		ProfitFactor: profitFactor,
		Metadata: map[string]string{
			"summary_version": "1",
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}

func isExitFill(event strategy.StrategyEvent) bool {
	if event.Metadata == nil {
		return false
	}
	return event.Metadata["exit_reason"] != ""
}

func parseEventFloat(value string) (float64, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func formatSummaryFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

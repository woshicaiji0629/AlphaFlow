package simulator

import (
	"strconv"

	"alphaflow/go-service/backtest-engine/internal/report"
	"alphaflow/go-service/pkg/strategy"
)

type SummaryOptions struct {
	RunID        string
	StrategySet  string
	Exchange     string
	Market       string
	Symbols      []string
	AccountCurve []report.AccountEquityPoint
	StartTime    int64
	EndTime      int64
	CreatedAt    int64
	UpdatedAt    int64
}

type AccountSummary struct {
	InitialEquity    float64
	FinalBalance     float64
	FinalEquity      float64
	AvailableBalance float64
	UsedMargin       float64
	RealizedPnL      float64
	UnrealizedPnL    float64
	TotalFee         float64
	TotalRebate      float64
	ReturnPct        float64
	MaxDrawdown      float64
	Liquidated       bool
	StoppedReason    string
}

func BuildBacktestRunSummary(events []strategy.StrategyEvent, options SummaryOptions) strategy.BacktestRunSummary {
	trades, err := BuildBacktestTrades(events)
	var tradeMetrics report.TradeMetrics
	var metricsErr error
	if err == nil {
		tradeMetrics, metricsErr = report.BuildTradeMetrics(trades)
	}
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
	metadata := map[string]string{
		"summary_version": "1",
	}
	maxDrawdown := ""
	if metricsErr == nil {
		maxDrawdown = report.FormatFloat(tradeMetrics.MaxDrawdown)
		metadata["gross_profit"] = report.FormatFloat(tradeMetrics.GrossProfit)
		metadata["gross_loss"] = report.FormatFloat(tradeMetrics.GrossLoss)
		metadata["winning_trades"] = strconv.FormatInt(tradeMetrics.WinningTrades, 10)
		metadata["losing_trades"] = strconv.FormatInt(tradeMetrics.LosingTrades, 10)
		metadata["flat_trades"] = strconv.FormatInt(tradeMetrics.FlatTrades, 10)
		metadata["max_consecutive_losses"] = strconv.FormatInt(tradeMetrics.MaxConsecutiveLosses, 10)
	}
	if err != nil {
		metadata["trade_pairing_error"] = err.Error()
	}
	if metricsErr != nil {
		metadata["report_error"] = metricsErr.Error()
	}
	if accountSummary, ok := BuildAccountSummary(options.AccountCurve); ok {
		totalPnL = accountSummary.FinalEquity - accountSummary.InitialEquity
		maxDrawdown = report.FormatFloat(accountSummary.MaxDrawdown)
		metadata["initial_equity"] = report.FormatFloat(accountSummary.InitialEquity)
		metadata["final_balance"] = report.FormatFloat(accountSummary.FinalBalance)
		metadata["final_equity"] = report.FormatFloat(accountSummary.FinalEquity)
		metadata["available_balance"] = report.FormatFloat(accountSummary.AvailableBalance)
		metadata["used_margin"] = report.FormatFloat(accountSummary.UsedMargin)
		metadata["account_realized_pnl"] = report.FormatFloat(accountSummary.RealizedPnL)
		metadata["account_unrealized_pnl"] = report.FormatFloat(accountSummary.UnrealizedPnL)
		metadata["total_fee"] = report.FormatFloat(accountSummary.TotalFee)
		metadata["total_rebate"] = report.FormatFloat(accountSummary.TotalRebate)
		metadata["account_return_pct"] = report.FormatFloat(accountSummary.ReturnPct)
		metadata["liquidated"] = strconv.FormatBool(accountSummary.Liquidated)
		if accountSummary.StoppedReason != "" {
			metadata["stopped_reason"] = accountSummary.StoppedReason
		}
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
		MaxDrawdown:  maxDrawdown,
		ProfitFactor: profitFactor,
		Metadata:     metadata,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
}

func BuildAccountSummary(curve []report.AccountEquityPoint) (AccountSummary, bool) {
	if len(curve) == 0 {
		return AccountSummary{}, false
	}
	initialEquity := curve[0].InitialEquity
	if initialEquity <= 0 {
		initialEquity = curve[0].Equity
	}
	peak := initialEquity
	maxDrawdown := 0.0
	for _, point := range curve {
		if point.Equity > peak {
			peak = point.Equity
		}
		drawdown := peak - point.Equity
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
	}
	final := curve[len(curve)-1]
	return AccountSummary{
		InitialEquity:    initialEquity,
		FinalBalance:     final.Balance,
		FinalEquity:      final.Equity,
		AvailableBalance: final.AvailableBalance,
		UsedMargin:       final.UsedMargin,
		RealizedPnL:      final.RealizedPnL,
		UnrealizedPnL:    final.UnrealizedPnL,
		TotalFee:         final.Fee,
		TotalRebate:      final.Rebate,
		ReturnPct:        final.ReturnPct,
		MaxDrawdown:      maxDrawdown,
		Liquidated:       final.Liquidated,
		StoppedReason:    final.StoppedReason,
	}, true
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

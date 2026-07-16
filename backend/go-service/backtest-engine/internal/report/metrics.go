package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

type EquityPoint struct {
	TradeID string  `json:"trade_id"`
	Time    int64   `json:"time"`
	Equity  float64 `json:"equity"`
}

type BarEquityPoint struct {
	Time          int64   `json:"time"`
	Symbol        string  `json:"symbol"`
	Price         float64 `json:"price"`
	RealizedPnL   float64 `json:"realized_pnl"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	Equity        float64 `json:"equity"`
}

type PortfolioEquityPoint struct {
	Time          int64   `json:"time"`
	RealizedPnL   float64 `json:"realized_pnl"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	Equity        float64 `json:"equity"`
}

type AccountEquityPoint struct {
	Time             int64   `json:"time"`
	InitialEquity    float64 `json:"initial_equity"`
	Balance          float64 `json:"balance"`
	AvailableBalance float64 `json:"available_balance"`
	UsedMargin       float64 `json:"used_margin"`
	RealizedPnL      float64 `json:"realized_pnl"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	Fee              float64 `json:"fee"`
	Rebate           float64 `json:"rebate"`
	Equity           float64 `json:"equity"`
	ReturnPct        float64 `json:"return_pct"`
	Liquidated       bool    `json:"liquidated"`
	StoppedReason    string  `json:"stopped_reason"`
}

type TradeMetrics struct {
	TotalTrades          int64         `json:"total_trades"`
	WinningTrades        int64         `json:"winning_trades"`
	LosingTrades         int64         `json:"losing_trades"`
	FlatTrades           int64         `json:"flat_trades"`
	WinRate              float64       `json:"win_rate"`
	NetPnL               float64       `json:"net_pnl"`
	GrossProfit          float64       `json:"gross_profit"`
	GrossLoss            float64       `json:"gross_loss"`
	ProfitFactor         float64       `json:"profit_factor"`
	MaxDrawdown          float64       `json:"max_drawdown"`
	MaxConsecutiveLosses int64         `json:"max_consecutive_losses"`
	EquityCurve          []EquityPoint `json:"equity_curve"`
}

type RunStats struct {
	Contexts      int `json:"contexts"`
	Decisions     int `json:"decisions"`
	Results       int `json:"results"`
	Events        int `json:"events"`
	OrderFills    int `json:"order_fills"`
	OpenPositions int `json:"open_positions"`
}

type BacktestReport struct {
	Summary              strategy.BacktestRunSummary `json:"summary"`
	Stats                RunStats                    `json:"stats"`
	Metrics              TradeMetrics                `json:"metrics"`
	BarEquityCurve       []BarEquityPoint            `json:"bar_equity_curve"`
	PortfolioEquityCurve []PortfolioEquityPoint      `json:"portfolio_equity_curve"`
	AccountEquityCurve   []AccountEquityPoint        `json:"account_equity_curve"`
}

type BacktestSummaryDTO struct {
	RunID         string            `json:"run_id"`
	Status        string            `json:"status"`
	StrategySet   string            `json:"strategy_set"`
	Exchange      string            `json:"exchange"`
	Market        string            `json:"market"`
	Symbols       []string          `json:"symbols"`
	StartTime     int64             `json:"start_time"`
	EndTime       int64             `json:"end_time"`
	TotalTrades   int64             `json:"total_trades"`
	WinRate       float64           `json:"win_rate"`
	NetPnL        string            `json:"net_pnl"`
	MaxDrawdown   string            `json:"max_drawdown"`
	ProfitFactor  float64           `json:"profit_factor"`
	Sharpe        float64           `json:"sharpe"`
	FailureReason string            `json:"failure_reason"`
	Metadata      map[string]string `json:"metadata"`
	CreatedAt     int64             `json:"created_at"`
	UpdatedAt     int64             `json:"updated_at"`
}

type BacktestReportDTO struct {
	Summary              BacktestSummaryDTO     `json:"summary"`
	Stats                RunStats               `json:"stats"`
	Metrics              TradeMetrics           `json:"metrics"`
	BarEquityCurve       []BarEquityPoint       `json:"bar_equity_curve"`
	PortfolioEquityCurve []PortfolioEquityPoint `json:"portfolio_equity_curve"`
	AccountEquityCurve   []AccountEquityPoint   `json:"account_equity_curve"`
}

func BuildTradeMetrics(trades []strategy.BacktestTrade) (TradeMetrics, error) {
	ordered := append([]strategy.BacktestTrade(nil), trades...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ExitTime == ordered[j].ExitTime {
			return ordered[i].TradeID < ordered[j].TradeID
		}
		return ordered[i].ExitTime < ordered[j].ExitTime
	})

	metrics := TradeMetrics{
		TotalTrades: int64(len(ordered)),
		EquityCurve: make([]EquityPoint, 0, len(ordered)),
	}
	equity := 0.0
	peak := 0.0
	currentLosses := int64(0)
	for _, trade := range ordered {
		pnl, err := parseFloat(trade.PnL)
		if err != nil {
			return TradeMetrics{}, fmt.Errorf("parse trade pnl trade_id=%s: %w", trade.TradeID, err)
		}
		metrics.NetPnL += pnl
		if pnl > 0 {
			metrics.WinningTrades++
			metrics.GrossProfit += pnl
			currentLosses = 0
		} else if pnl < 0 {
			metrics.LosingTrades++
			metrics.GrossLoss += -pnl
			currentLosses++
			if currentLosses > metrics.MaxConsecutiveLosses {
				metrics.MaxConsecutiveLosses = currentLosses
			}
		} else {
			metrics.FlatTrades++
			currentLosses = 0
		}

		equity += pnl
		if equity > peak {
			peak = equity
		}
		drawdown := peak - equity
		if drawdown > metrics.MaxDrawdown {
			metrics.MaxDrawdown = drawdown
		}
		metrics.EquityCurve = append(metrics.EquityCurve, EquityPoint{
			TradeID: trade.TradeID,
			Time:    trade.ExitTime,
			Equity:  equity,
		})
	}
	if metrics.TotalTrades > 0 {
		metrics.WinRate = float64(metrics.WinningTrades) / float64(metrics.TotalTrades)
	}
	if metrics.GrossLoss > 0 {
		metrics.ProfitFactor = metrics.GrossProfit / metrics.GrossLoss
	} else if metrics.GrossProfit > 0 {
		metrics.ProfitFactor = math.Inf(1)
	}
	return metrics, nil
}

func BuildBacktestReport(
	summary strategy.BacktestRunSummary,
	stats RunStats,
	trades []strategy.BacktestTrade,
	barEquityCurve ...[]BarEquityPoint,
) (BacktestReport, error) {
	metrics, err := BuildTradeMetrics(trades)
	if err != nil {
		return BacktestReport{}, err
	}
	if summary.MaxDrawdown == "" {
		summary.MaxDrawdown = FormatFloat(metrics.MaxDrawdown)
	}
	if summary.Metadata == nil {
		summary.Metadata = map[string]string{}
	}
	if summary.Metadata["gross_profit"] == "" {
		summary.Metadata["gross_profit"] = FormatFloat(metrics.GrossProfit)
	}
	if summary.Metadata["gross_loss"] == "" {
		summary.Metadata["gross_loss"] = FormatFloat(metrics.GrossLoss)
	}
	if summary.Metadata["winning_trades"] == "" {
		summary.Metadata["winning_trades"] = strconv.FormatInt(metrics.WinningTrades, 10)
	}
	if summary.Metadata["losing_trades"] == "" {
		summary.Metadata["losing_trades"] = strconv.FormatInt(metrics.LosingTrades, 10)
	}
	if summary.Metadata["flat_trades"] == "" {
		summary.Metadata["flat_trades"] = strconv.FormatInt(metrics.FlatTrades, 10)
	}
	if summary.Metadata["max_consecutive_losses"] == "" {
		summary.Metadata["max_consecutive_losses"] = strconv.FormatInt(metrics.MaxConsecutiveLosses, 10)
	}
	item := BacktestReport{
		Summary: summary,
		Stats:   stats,
		Metrics: metrics,
	}
	if len(barEquityCurve) > 0 {
		item.BarEquityCurve = append([]BarEquityPoint(nil), barEquityCurve[0]...)
		item.PortfolioEquityCurve = BuildPortfolioEquityCurve(item.BarEquityCurve)
	}
	return item, nil
}

func BuildBacktestReportWithInitialEquity(
	summary strategy.BacktestRunSummary,
	stats RunStats,
	trades []strategy.BacktestTrade,
	initialEquity float64,
	barEquityCurve ...[]BarEquityPoint,
) (BacktestReport, error) {
	item, err := BuildBacktestReport(summary, stats, trades, barEquityCurve...)
	if err != nil {
		return BacktestReport{}, err
	}
	item.AccountEquityCurve = BuildAccountEquityCurve(initialEquity, item.PortfolioEquityCurve)
	return item, nil
}

func BuildPortfolioEquityCurve(points []BarEquityPoint) []PortfolioEquityPoint {
	if len(points) == 0 {
		return nil
	}
	ordered := append([]BarEquityPoint(nil), points...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Time < ordered[j].Time
	})

	latestUnrealizedBySymbol := map[string]float64{}
	curve := make([]PortfolioEquityPoint, 0, len(ordered))
	for index := 0; index < len(ordered); {
		currentTime := ordered[index].Time
		realizedPnL := ordered[index].RealizedPnL
		for index < len(ordered) && ordered[index].Time == currentTime {
			point := ordered[index]
			latestUnrealizedBySymbol[point.Symbol] = point.UnrealizedPnL
			realizedPnL = point.RealizedPnL
			index++
		}
		unrealizedPnL := 0.0
		for _, value := range latestUnrealizedBySymbol {
			unrealizedPnL += value
		}
		curve = append(curve, PortfolioEquityPoint{
			Time:          currentTime,
			RealizedPnL:   realizedPnL,
			UnrealizedPnL: unrealizedPnL,
			Equity:        realizedPnL + unrealizedPnL,
		})
	}
	return curve
}

func BuildAccountEquityCurve(initialEquity float64, points []PortfolioEquityPoint) []AccountEquityPoint {
	if initialEquity <= 0 || len(points) == 0 {
		return nil
	}
	curve := make([]AccountEquityPoint, 0, len(points))
	for _, point := range points {
		accountEquity := initialEquity + point.Equity
		curve = append(curve, AccountEquityPoint{
			Time:             point.Time,
			InitialEquity:    initialEquity,
			Balance:          initialEquity + point.RealizedPnL,
			AvailableBalance: accountEquity,
			RealizedPnL:      point.RealizedPnL,
			UnrealizedPnL:    point.UnrealizedPnL,
			Equity:           accountEquity,
			ReturnPct:        point.Equity / initialEquity * 100,
		})
	}
	return curve
}

func FormatBacktestReport(report BacktestReport) string {
	summary := report.Summary
	stats := report.Stats
	var builder strings.Builder
	builder.WriteString("Backtest report\n")
	builder.WriteString("run_id: ")
	builder.WriteString(summary.RunID)
	builder.WriteString("\nstrategy_set: ")
	builder.WriteString(summary.StrategySet)
	builder.WriteString("\nsymbols: ")
	builder.WriteString(strings.Join(summary.Symbols, ","))
	builder.WriteString("\ncontexts: ")
	builder.WriteString(strconv.Itoa(stats.Contexts))
	builder.WriteString("\ndecisions: ")
	builder.WriteString(strconv.Itoa(stats.Decisions))
	builder.WriteString("\nresults: ")
	builder.WriteString(strconv.Itoa(stats.Results))
	builder.WriteString("\nevents: ")
	builder.WriteString(strconv.Itoa(stats.Events))
	builder.WriteString("\norder_fills: ")
	builder.WriteString(strconv.Itoa(stats.OrderFills))
	builder.WriteString("\nopen_positions: ")
	builder.WriteString(strconv.Itoa(stats.OpenPositions))
	builder.WriteString("\ntotal_trades: ")
	builder.WriteString(strconv.FormatInt(summary.TotalTrades, 10))
	builder.WriteString("\nwin_rate: ")
	builder.WriteString(FormatFloat(summary.WinRate))
	builder.WriteString("\nnet_pnl: ")
	builder.WriteString(summary.NetPnL)
	builder.WriteString("\nmax_drawdown: ")
	builder.WriteString(summary.MaxDrawdown)
	builder.WriteString("\nprofit_factor: ")
	builder.WriteString(FormatFloat(summary.ProfitFactor))
	builder.WriteString("\nmax_consecutive_losses: ")
	builder.WriteString(summary.Metadata["max_consecutive_losses"])
	if value := summary.Metadata["final_equity"]; value != "" {
		builder.WriteString("\nfinal_equity: ")
		builder.WriteString(value)
	}
	if value := summary.Metadata["account_return_pct"]; value != "" {
		builder.WriteString("\naccount_return_pct: ")
		builder.WriteString(value)
	}
	if value := summary.Metadata["total_fee"]; value != "" {
		builder.WriteString("\ntotal_fee: ")
		builder.WriteString(value)
	}
	if value := summary.Metadata["total_rebate"]; value != "" {
		builder.WriteString("\ntotal_rebate: ")
		builder.WriteString(value)
	}
	if value := summary.Metadata["liquidated"]; value != "" {
		builder.WriteString("\nliquidated: ")
		builder.WriteString(value)
	}
	if value := summary.Metadata["stopped_reason"]; value != "" {
		builder.WriteString("\nstopped_reason: ")
		builder.WriteString(value)
	}
	return builder.String()
}

func FormatRunSummary(summary strategy.BacktestRunSummary, stats RunStats) string {
	return FormatBacktestReport(BacktestReport{
		Summary: summary,
		Stats:   stats,
	})
}

func ToBacktestSummaryDTO(summary strategy.BacktestRunSummary) BacktestSummaryDTO {
	return BacktestSummaryDTO{
		RunID:         summary.RunID,
		Status:        string(summary.Status),
		StrategySet:   summary.StrategySet,
		Exchange:      summary.Exchange,
		Market:        summary.Market,
		Symbols:       append([]string(nil), summary.Symbols...),
		StartTime:     summary.StartTime,
		EndTime:       summary.EndTime,
		TotalTrades:   summary.TotalTrades,
		WinRate:       finiteFloat(summary.WinRate),
		NetPnL:        summary.NetPnL,
		MaxDrawdown:   summary.MaxDrawdown,
		ProfitFactor:  finiteFloat(summary.ProfitFactor),
		Sharpe:        finiteFloat(summary.Sharpe),
		FailureReason: summary.FailureReason,
		Metadata:      copyStringMap(summary.Metadata),
		CreatedAt:     summary.CreatedAt,
		UpdatedAt:     summary.UpdatedAt,
	}
}

func ToBacktestReportDTO(report BacktestReport) BacktestReportDTO {
	report.Metrics = finiteMetrics(report.Metrics)
	return BacktestReportDTO{
		Summary:              ToBacktestSummaryDTO(report.Summary),
		Stats:                report.Stats,
		Metrics:              report.Metrics,
		BarEquityCurve:       finiteBarEquityCurve(report.BarEquityCurve),
		PortfolioEquityCurve: finitePortfolioEquityCurve(report.PortfolioEquityCurve),
		AccountEquityCurve:   finiteAccountEquityCurve(report.AccountEquityCurve),
	}
}

func MarshalBacktestReport(report BacktestReport) ([]byte, error) {
	return json.MarshalIndent(ToBacktestReportDTO(report), "", "  ")
}

const backtestReportCurveChunkSize = 4096

// WriteBacktestReport writes the same indented JSON representation as
// MarshalBacktestReport without retaining full curve copies or the complete
// encoded payload in memory.
func WriteBacktestReport(writer io.Writer, report BacktestReport) error {
	if writer == nil {
		return fmt.Errorf("nil backtest report writer")
	}
	output := &backtestReportJSONWriter{writer: bufio.NewWriterSize(writer, 64*1024)}
	output.writeString("{\n")
	output.writeString(`  "summary": `)
	output.writeJSON(ToBacktestSummaryDTO(report.Summary), "  ")
	output.writeString(",\n")
	output.writeString(`  "stats": `)
	output.writeJSON(report.Stats, "  ")
	output.writeString(",\n")
	output.writeString(`  "metrics": `)
	output.writeJSON(finiteMetrics(report.Metrics), "  ")
	output.writeString(",\n")
	output.writeString(`  "bar_equity_curve": `)
	writeBacktestReportCurve(output, report.BarEquityCurve, finiteBarEquityPoint)
	output.writeString(",\n")
	output.writeString(`  "portfolio_equity_curve": `)
	writeBacktestReportCurve(output, report.PortfolioEquityCurve, finitePortfolioEquityPoint)
	output.writeString(",\n")
	output.writeString(`  "account_equity_curve": `)
	writeBacktestReportCurve(output, report.AccountEquityCurve, finiteAccountEquityPoint)
	output.writeString("\n}")
	if output.err != nil {
		return output.err
	}
	return output.writer.Flush()
}

type backtestReportJSONWriter struct {
	writer *bufio.Writer
	err    error
}

func (w *backtestReportJSONWriter) writeString(value string) {
	if w.err != nil {
		return
	}
	_, w.err = w.writer.WriteString(value)
}

func (w *backtestReportJSONWriter) writeBytes(value []byte) {
	if w.err != nil {
		return
	}
	_, w.err = w.writer.Write(value)
}

func (w *backtestReportJSONWriter) writeJSON(value any, lineIndent string) {
	if w.err != nil {
		return
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		w.err = err
		return
	}
	w.writeIndentedBytes(payload, lineIndent)
}

func (w *backtestReportJSONWriter) writeIndentedBytes(value []byte, lineIndent string) {
	start := 0
	for index, current := range value {
		if current != '\n' {
			continue
		}
		w.writeBytes(value[start : index+1])
		w.writeString(lineIndent)
		start = index + 1
	}
	w.writeBytes(value[start:])
}

func writeBacktestReportCurve[T any](
	output *backtestReportJSONWriter,
	points []T,
	finitePoint func(T) T,
) {
	if output.err != nil {
		return
	}
	if points == nil {
		output.writeString("null")
		return
	}
	if len(points) == 0 {
		output.writeString("[]")
		return
	}
	output.writeString("[\n")
	chunkSize := backtestReportCurveChunkSize
	if len(points) < chunkSize {
		chunkSize = len(points)
	}
	chunk := make([]T, chunkSize)
	for start := 0; start < len(points); start += chunkSize {
		end := start + chunkSize
		if end > len(points) {
			end = len(points)
		}
		current := chunk[:end-start]
		for index := range current {
			current[index] = finitePoint(points[start+index])
		}
		payload, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			output.err = err
			return
		}
		if len(payload) < 4 || payload[0] != '[' || payload[1] != '\n' ||
			payload[len(payload)-2] != '\n' || payload[len(payload)-1] != ']' {
			output.err = fmt.Errorf("unexpected backtest report curve encoding")
			return
		}
		output.writeString("  ")
		output.writeIndentedBytes(payload[2:len(payload)-2], "  ")
		if end < len(points) {
			output.writeString(",\n")
		} else {
			output.writeString("\n")
		}
	}
	output.writeString("  ]")
}

func FormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func finiteFloat(value float64) float64 {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return 0
	}
	return value
}

func finiteMetrics(metrics TradeMetrics) TradeMetrics {
	metrics.WinRate = finiteFloat(metrics.WinRate)
	metrics.NetPnL = finiteFloat(metrics.NetPnL)
	metrics.GrossProfit = finiteFloat(metrics.GrossProfit)
	metrics.GrossLoss = finiteFloat(metrics.GrossLoss)
	metrics.ProfitFactor = finiteFloat(metrics.ProfitFactor)
	metrics.MaxDrawdown = finiteFloat(metrics.MaxDrawdown)
	for index := range metrics.EquityCurve {
		metrics.EquityCurve[index].Equity = finiteFloat(metrics.EquityCurve[index].Equity)
	}
	return metrics
}

func finiteBarEquityCurve(points []BarEquityPoint) []BarEquityPoint {
	if points == nil {
		return nil
	}
	copied := make([]BarEquityPoint, len(points))
	for index, point := range points {
		copied[index] = finiteBarEquityPoint(point)
	}
	return copied
}

func finiteBarEquityPoint(point BarEquityPoint) BarEquityPoint {
	point.Price = finiteFloat(point.Price)
	point.RealizedPnL = finiteFloat(point.RealizedPnL)
	point.UnrealizedPnL = finiteFloat(point.UnrealizedPnL)
	point.Equity = finiteFloat(point.Equity)
	return point
}

func finitePortfolioEquityCurve(points []PortfolioEquityPoint) []PortfolioEquityPoint {
	if points == nil {
		return nil
	}
	copied := make([]PortfolioEquityPoint, len(points))
	for index, point := range points {
		copied[index] = finitePortfolioEquityPoint(point)
	}
	return copied
}

func finitePortfolioEquityPoint(point PortfolioEquityPoint) PortfolioEquityPoint {
	point.RealizedPnL = finiteFloat(point.RealizedPnL)
	point.UnrealizedPnL = finiteFloat(point.UnrealizedPnL)
	point.Equity = finiteFloat(point.Equity)
	return point
}

func finiteAccountEquityCurve(points []AccountEquityPoint) []AccountEquityPoint {
	if points == nil {
		return nil
	}
	copied := make([]AccountEquityPoint, len(points))
	for index, point := range points {
		copied[index] = finiteAccountEquityPoint(point)
	}
	return copied
}

func finiteAccountEquityPoint(point AccountEquityPoint) AccountEquityPoint {
	point.InitialEquity = finiteFloat(point.InitialEquity)
	point.Balance = finiteFloat(point.Balance)
	point.AvailableBalance = finiteFloat(point.AvailableBalance)
	point.UsedMargin = finiteFloat(point.UsedMargin)
	point.RealizedPnL = finiteFloat(point.RealizedPnL)
	point.UnrealizedPnL = finiteFloat(point.UnrealizedPnL)
	point.Fee = finiteFloat(point.Fee)
	point.Rebate = finiteFloat(point.Rebate)
	point.Equity = finiteFloat(point.Equity)
	point.ReturnPct = finiteFloat(point.ReturnPct)
	return point
}

func copyStringMap(items map[string]string) map[string]string {
	if items == nil {
		return nil
	}
	copied := make(map[string]string, len(items))
	for key, value := range items {
		copied[key] = value
	}
	return copied
}

func parseFloat(value string) (float64, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

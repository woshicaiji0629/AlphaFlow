package signalresearch

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/strategy"
)

type SinglePositionConfig struct {
	InitialEquity       float64
	MarginQuote         float64
	Leverage            float64
	InitialStopBps      float64
	BreakEvenTriggerBps float64
	BreakEvenFloorBps   float64
	TrailingTriggerBps  float64
	TrailingDrawdownBps float64
	MaxHolding          time.Duration
	CooldownBars        int
	FeeRate             float64
	SlippageBps         float64
}

type SinglePositionSummary struct {
	Trades               int            `json:"trades"`
	WinningTrades        int            `json:"winning_trades"`
	LosingTrades         int            `json:"losing_trades"`
	WinRate              float64        `json:"win_rate"`
	NetPnL               float64        `json:"net_pnl"`
	ReturnPct            float64        `json:"return_pct"`
	GrossProfit          float64        `json:"gross_profit"`
	GrossLoss            float64        `json:"gross_loss"`
	ProfitFactor         float64        `json:"profit_factor"`
	MaxDrawdown          float64        `json:"max_drawdown"`
	MaxDrawdownPct       float64        `json:"max_drawdown_pct"`
	MaxConsecutiveLosses int            `json:"max_consecutive_losses"`
	AverageHoldingMin    float64        `json:"average_holding_minutes"`
	TradingCost          float64        `json:"trading_cost"`
	SkippedNoRegime      int            `json:"skipped_no_regime"`
	SkippedByRegime      int            `json:"skipped_by_regime"`
	RegimeSkipReasons    map[string]int `json:"regime_skip_reasons,omitempty"`
	SkippedWhileOpen     int            `json:"skipped_while_open"`
	SkippedCooldown      int            `json:"skipped_cooldown"`
	SkippedConflict      int            `json:"skipped_conflict"`
	InitialStopExits     int            `json:"initial_stop_exits"`
	BreakEvenExits       int            `json:"break_even_exits"`
	TrailingStopExits    int            `json:"trailing_stop_exits"`
	MaxHoldingExits      int            `json:"max_holding_exits"`
	DatasetEndExits      int            `json:"dataset_end_exits"`
	AmbiguousStopExits   int            `json:"same_bar_ambiguous_exits"`
	LossNoProfit         int            `json:"loss_no_profit"`
	LossSmallProfit      int            `json:"loss_small_profit"`
	LossGiveback         int            `json:"loss_giveback"`
	LossTimeout          int            `json:"loss_timeout"`
	AverageMFEBps        float64        `json:"average_mfe_bps"`
	AverageMAEBps        float64        `json:"average_mae_bps"`
	AverageGivebackBps   float64        `json:"average_giveback_bps"`
	LosingAverageMFEBps  float64        `json:"losing_average_mfe_bps"`
	WinningMAEP50Bps     float64        `json:"winning_mae_p50_bps"`
	WinningMAEP75Bps     float64        `json:"winning_mae_p75_bps"`
	WinningMAEP90Bps     float64        `json:"winning_mae_p90_bps"`
	LosingMFEP50Bps      float64        `json:"losing_mfe_p50_bps"`
	LosingMFEP75Bps      float64        `json:"losing_mfe_p75_bps"`
	LosingMFEP90Bps      float64        `json:"losing_mfe_p90_bps"`
}

type SinglePositionTrade struct {
	Side              strategy.SignalSide    `json:"side"`
	EntryTimeMS       int64                  `json:"entry_time_ms"`
	ExitTimeMS        int64                  `json:"exit_time_ms"`
	EntryPrice        float64                `json:"entry_price"`
	ExitReason        string                 `json:"exit_reason"`
	GrossBps          float64                `json:"gross_bps"`
	NetPnL            float64                `json:"net_pnl"`
	MFEBps            float64                `json:"mfe_bps"`
	MAEBps            float64                `json:"mae_bps"`
	GivebackBps       float64                `json:"giveback_bps"`
	HoldingMinutes    float64                `json:"holding_minutes"`
	EntryState        marketregime.State     `json:"entry_state"`
	EntryDirection    marketregime.Direction `json:"entry_direction"`
	TrendabilityScore float64                `json:"trendability_score"`
	DirectionScore    float64                `json:"direction_score"`
}

type singlePosition struct {
	side          strategy.SignalSide
	entryPrice    float64
	entryTimeMS   int64
	stopBps       float64
	peakFavorable float64
	maxAdverse    float64
	entryRegime   marketregime.Result
}

type SinglePositionReplay struct {
	config            SinglePositionConfig
	position          *singlePosition
	cooldownRemaining int
	equity            float64
	peakEquity        float64
	lastClose         float64
	lastCloseTimeMS   int64
	consecutiveLosses int
	totalHoldingMS    int64
	totalMFE          float64
	totalMAE          float64
	totalGiveback     float64
	losingMFE         float64
	trades            []SinglePositionTrade
	summary           SinglePositionSummary
}

func NewSinglePositionReplay(config SinglePositionConfig) (*SinglePositionReplay, error) {
	if config.InitialEquity <= 0 || config.MarginQuote <= 0 || config.Leverage <= 0 ||
		config.InitialStopBps <= 0 || config.BreakEvenTriggerBps <= 0 || config.BreakEvenFloorBps < 0 ||
		config.TrailingTriggerBps <= 0 || config.TrailingDrawdownBps <= 0 ||
		config.MaxHolding <= 0 || config.CooldownBars < 0 || config.FeeRate < 0 || config.SlippageBps < 0 {
		return nil, fmt.Errorf("invalid single position replay config")
	}
	if config.BreakEvenFloorBps >= config.BreakEvenTriggerBps || config.TrailingTriggerBps < config.BreakEvenTriggerBps {
		return nil, fmt.Errorf("invalid single position protection thresholds")
	}
	return &SinglePositionReplay{config: config, equity: config.InitialEquity, peakEquity: config.InitialEquity}, nil
}

// Advance evaluates only protective levels established before this bar. New
// peaks update the stop for the next bar, avoiding optimistic intrabar ordering.
func (r *SinglePositionReplay) Advance(kline marketmodel.Kline) error {
	high, err := parsePositivePrice("high", kline.High)
	if err != nil {
		return err
	}
	low, err := parsePositivePrice("low", kline.Low)
	if err != nil {
		return err
	}
	closePrice, err := parsePositivePrice("close", kline.Close)
	if err != nil {
		return err
	}
	r.lastClose, r.lastCloseTimeMS = closePrice, kline.CloseTime
	if r.position == nil {
		if r.cooldownRemaining > 0 {
			r.cooldownRemaining--
		}
		return nil
	}
	favorable, adverse := excursionBps(r.position.side, r.position.entryPrice, high, low)
	r.position.peakFavorable = math.Max(r.position.peakFavorable, favorable)
	r.position.maxAdverse = math.Max(r.position.maxAdverse, adverse)
	stopTouched := worstGrossBps(r.position.side, r.position.entryPrice, high, low) <= r.position.stopBps
	if stopTouched {
		reason := "initial_stop"
		if r.position.stopBps >= r.config.TrailingTriggerBps-r.config.TrailingDrawdownBps {
			reason = "trailing_stop"
		} else if r.position.stopBps >= r.config.BreakEvenFloorBps {
			reason = "break_even"
		} else if favorable >= r.config.BreakEvenTriggerBps {
			reason = "same_bar_ambiguous_stop"
		}
		r.close(r.position.stopBps, kline.CloseTime, reason)
		return nil
	}
	if kline.CloseTime-r.position.entryTimeMS >= r.config.MaxHolding.Milliseconds() {
		r.close(directionalReturnBps(r.position.side, r.position.entryPrice, closePrice), kline.CloseTime, "max_holding")
		return nil
	}
	if r.position.peakFavorable >= r.config.BreakEvenTriggerBps {
		r.position.stopBps = math.Max(r.position.stopBps, r.config.BreakEvenFloorBps)
	}
	if r.position.peakFavorable >= r.config.TrailingTriggerBps {
		r.position.stopBps = math.Max(r.position.stopBps, r.position.peakFavorable-r.config.TrailingDrawdownBps)
	}
	return nil
}

func (r *SinglePositionReplay) TryEnter(snapshot strategy.Snapshot, side strategy.SignalSide, regime *marketregime.Result) (bool, error) {
	if regime == nil {
		r.summary.SkippedNoRegime++
		return false, nil
	}
	if side != strategy.SignalSideBuy && side != strategy.SignalSideSell {
		return false, fmt.Errorf("unsupported single position side %q", side)
	}
	if side == strategy.SignalSideBuy && !regime.AllowLong || side == strategy.SignalSideSell && !regime.AllowShort {
		r.summary.SkippedByRegime++
		if reason := regimeSkipReason(side, *regime); reason != "" {
			if r.summary.RegimeSkipReasons == nil {
				r.summary.RegimeSkipReasons = make(map[string]int)
			}
			r.summary.RegimeSkipReasons[reason]++
		}
		return false, nil
	}
	if r.position != nil {
		r.summary.SkippedWhileOpen++
		return false, nil
	}
	if r.cooldownRemaining > 0 {
		r.summary.SkippedCooldown++
		return false, nil
	}
	entryPrice, err := parsePositivePrice("entry", snapshot.Current.Close)
	if err != nil {
		return false, err
	}
	r.position = &singlePosition{
		side: side, entryPrice: entryPrice, entryTimeMS: snapshot.Current.CloseTime,
		stopBps: -r.config.InitialStopBps, entryRegime: *regime,
	}
	return true, nil
}

func regimeSkipReason(side strategy.SignalSide, regime marketregime.Result) string {
	if side == strategy.SignalSideBuy && !regime.AllowLong && regime.AllowShort ||
		side == strategy.SignalSideSell && !regime.AllowShort && regime.AllowLong {
		return "v4_countertrend_signal"
	}
	for index := len(regime.Reasons) - 1; index >= 0; index-- {
		if (strings.HasPrefix(regime.Reasons[index], "v4_") || strings.HasPrefix(regime.Reasons[index], "v5_") || strings.HasPrefix(regime.Reasons[index], "v6_")) &&
			regime.Reasons[index] != "v4_permitted" && regime.Reasons[index] != "v4_release_confirmed" &&
			regime.Reasons[index] != "v5_permitted" && regime.Reasons[index] != "v5_fast_release_confirmed" &&
			regime.Reasons[index] != "v6_permitted" && regime.Reasons[index] != "v6_fast_release_confirmed" {
			return regime.Reasons[index]
		}
	}
	return ""
}

func (r *SinglePositionReplay) SkipConflict() { r.summary.SkippedConflict++ }

func (r *SinglePositionReplay) Finish() {
	if r.position != nil && r.lastClose > 0 {
		r.close(directionalReturnBps(r.position.side, r.position.entryPrice, r.lastClose), r.lastCloseTimeMS, "dataset_end")
	}
}

func (r *SinglePositionReplay) Summary() SinglePositionSummary {
	result := r.summary
	winningMAE := make([]float64, 0, result.WinningTrades)
	losingMFE := make([]float64, 0, result.LosingTrades)
	for _, trade := range r.trades {
		if trade.NetPnL > 0 {
			winningMAE = append(winningMAE, trade.MAEBps)
		} else if trade.NetPnL < 0 {
			losingMFE = append(losingMFE, trade.MFEBps)
		}
	}
	result.WinningMAEP50Bps = percentile(winningMAE, 0.50)
	result.WinningMAEP75Bps = percentile(winningMAE, 0.75)
	result.WinningMAEP90Bps = percentile(winningMAE, 0.90)
	result.LosingMFEP50Bps = percentile(losingMFE, 0.50)
	result.LosingMFEP75Bps = percentile(losingMFE, 0.75)
	result.LosingMFEP90Bps = percentile(losingMFE, 0.90)
	if result.Trades > 0 {
		result.WinRate = float64(result.WinningTrades) / float64(result.Trades)
		result.AverageHoldingMin = float64(r.totalHoldingMS) / float64(result.Trades) / float64(time.Minute.Milliseconds())
		result.AverageMFEBps = r.totalMFE / float64(result.Trades)
		result.AverageMAEBps = r.totalMAE / float64(result.Trades)
		result.AverageGivebackBps = r.totalGiveback / float64(result.Trades)
	}
	if result.LosingTrades > 0 {
		result.LosingAverageMFEBps = r.losingMFE / float64(result.LosingTrades)
	}
	if result.GrossLoss > 0 {
		result.ProfitFactor = result.GrossProfit / result.GrossLoss
	} else if result.GrossProfit > 0 {
		result.ProfitFactor = math.Inf(1)
	}
	result.ReturnPct = result.NetPnL / r.config.InitialEquity * 100
	return result
}

func percentile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	index := int(math.Ceil(fraction*float64(len(ordered)))) - 1
	index = max(0, min(index, len(ordered)-1))
	return ordered[index]
}

func (r *SinglePositionReplay) Trades() []SinglePositionTrade {
	return append([]SinglePositionTrade(nil), r.trades...)
}

func (r *SinglePositionReplay) close(grossBps float64, exitTimeMS int64, reason string) {
	notional := r.config.MarginQuote * r.config.Leverage
	costBps := r.config.FeeRate*2*10000 + r.config.SlippageBps*2
	tradingCost := notional * costBps / 10000
	pnl := notional*grossBps/10000 - tradingCost
	giveback := math.Max(0, r.position.peakFavorable-grossBps)
	holdingMinutes := float64(exitTimeMS-r.position.entryTimeMS) / float64(time.Minute.Milliseconds())
	r.trades = append(r.trades, SinglePositionTrade{
		Side: r.position.side, EntryTimeMS: r.position.entryTimeMS, ExitTimeMS: exitTimeMS,
		EntryPrice: r.position.entryPrice, ExitReason: reason, GrossBps: grossBps, NetPnL: pnl,
		MFEBps: r.position.peakFavorable, MAEBps: r.position.maxAdverse, GivebackBps: giveback,
		HoldingMinutes: holdingMinutes, EntryState: r.position.entryRegime.State,
		EntryDirection:    r.position.entryRegime.Direction,
		TrendabilityScore: r.position.entryRegime.TrendabilityScore,
		DirectionScore:    r.position.entryRegime.DirectionScore,
	})
	r.summary.Trades++
	r.summary.NetPnL += pnl
	r.summary.TradingCost += tradingCost
	if pnl > 0 {
		r.summary.WinningTrades++
		r.summary.GrossProfit += pnl
		r.consecutiveLosses = 0
	} else if pnl < 0 {
		r.summary.LosingTrades++
		r.summary.GrossLoss += -pnl
		r.consecutiveLosses++
		r.summary.MaxConsecutiveLosses = max(r.summary.MaxConsecutiveLosses, r.consecutiveLosses)
		r.losingMFE += r.position.peakFavorable
		switch {
		case reason == "max_holding":
			r.summary.LossTimeout++
		case reason == "same_bar_ambiguous_stop":
			// Kept separate from causal feature and stop failures.
		case r.position.peakFavorable < r.config.BreakEvenFloorBps:
			r.summary.LossNoProfit++
		case r.position.peakFavorable < r.config.BreakEvenTriggerBps:
			r.summary.LossSmallProfit++
		default:
			r.summary.LossGiveback++
		}
	}
	switch reason {
	case "initial_stop":
		r.summary.InitialStopExits++
	case "break_even":
		r.summary.BreakEvenExits++
	case "trailing_stop":
		r.summary.TrailingStopExits++
	case "max_holding":
		r.summary.MaxHoldingExits++
	case "dataset_end":
		r.summary.DatasetEndExits++
	case "same_bar_ambiguous_stop":
		r.summary.AmbiguousStopExits++
	}
	r.equity += pnl
	r.peakEquity = math.Max(r.peakEquity, r.equity)
	drawdown := r.peakEquity - r.equity
	if drawdown > r.summary.MaxDrawdown {
		r.summary.MaxDrawdown = drawdown
		r.summary.MaxDrawdownPct = drawdown / r.peakEquity * 100
	}
	r.totalHoldingMS += exitTimeMS - r.position.entryTimeMS
	r.totalMFE += r.position.peakFavorable
	r.totalMAE += r.position.maxAdverse
	r.totalGiveback += giveback
	r.position = nil
	r.cooldownRemaining = r.config.CooldownBars
}

func parsePositivePrice(name string, text string) (float64, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("parse single position %s %q", name, text)
	}
	return value, nil
}

func worstGrossBps(side strategy.SignalSide, entry float64, high float64, low float64) float64 {
	if side == strategy.SignalSideBuy {
		return (low - entry) / entry * 10000
	}
	return (entry - high) / entry * 10000
}

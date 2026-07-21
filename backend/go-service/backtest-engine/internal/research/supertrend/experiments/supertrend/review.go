package supertrend

import (
	"fmt"
	"math"
	"strconv"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

type stopReviewSource struct {
	mode    string
	trades  []signalresearch.SinglePositionTrade
	entries []entryDiagnostic
}

type stopReviewTrade struct {
	Mode                 string                             `json:"mode"`
	Trade                signalresearch.SinglePositionTrade `json:"trade"`
	Entry                *entryDiagnostic                   `json:"entry,omitempty"`
	PostStopFavorablePts float64                            `json:"post_stop_favorable_points"`
	PostStopAdversePts   float64                            `json:"post_stop_adverse_points"`
	PostStopBars         int                                `json:"post_stop_bars"`
	Reason               string                             `json:"reason"`
}

type stopReviewReport struct {
	ForwardBars   int               `json:"forward_bars"`
	Trades        []stopReviewTrade `json:"trades"`
	WinningTrades []stopReviewTrade `json:"winning_trades"`
	ModeCounts    map[string]int    `json:"mode_counts"`
	ReasonCounts  map[string]int    `json:"reason_counts"`
}

func validateSwingReviewConfig(config *signalresearch.SwingReviewConfig) error {
	if config == nil {
		return nil
	}
	if config.MinimumMovePoints <= 0 || config.ReversalPoints <= 0 || config.ReversalPoints >= config.MinimumMovePoints {
		return fmt.Errorf("invalid swing review thresholds")
	}
	if config.LeadWindowMS < 0 {
		return fmt.Errorf("swing review lead window cannot be negative")
	}
	return nil
}

func buildStopReview(bars []marketmodel.Kline, sources []stopReviewSource) (stopReviewReport, error) {
	const forwardBars = 20
	report := stopReviewReport{ForwardBars: forwardBars, ModeCounts: map[string]int{}, ReasonCounts: map[string]int{}}
	for _, source := range sources {
		for _, trade := range source.trades {
			entry := matchingEntry(source.entries, source.mode, trade.EntryTimeMS)
			if trade.NetPnL > 0 {
				report.WinningTrades = append(report.WinningTrades, stopReviewTrade{
					Mode: source.mode, Trade: trade, Entry: entry, Reason: "profitable",
				})
			}
			if trade.ExitReason != "initial_stop" {
				continue
			}
			item := stopReviewTrade{Mode: source.mode, Trade: trade, Entry: entry, PostStopBars: forwardBars}
			seen := 0
			for _, bar := range bars {
				if bar.CloseTime <= trade.ExitTimeMS || seen >= forwardBars {
					continue
				}
				high, err := strconv.ParseFloat(bar.High, 64)
				if err != nil {
					return stopReviewReport{}, fmt.Errorf("parse stop review high %q: %w", bar.High, err)
				}
				low, err := strconv.ParseFloat(bar.Low, 64)
				if err != nil {
					return stopReviewReport{}, fmt.Errorf("parse stop review low %q: %w", bar.Low, err)
				}
				if trade.Side == strategy.SignalSideBuy {
					item.PostStopFavorablePts = math.Max(item.PostStopFavorablePts, high-trade.EntryPrice)
					item.PostStopAdversePts = math.Max(item.PostStopAdversePts, trade.EntryPrice-low)
				} else {
					item.PostStopFavorablePts = math.Max(item.PostStopFavorablePts, trade.EntryPrice-low)
					item.PostStopAdversePts = math.Max(item.PostStopAdversePts, high-trade.EntryPrice)
				}
				seen++
			}
			item.PostStopBars = seen
			switch {
			case item.PostStopFavorablePts >= 30:
				item.Reason = "stop_too_tight_or_entry_timing"
			case item.PostStopAdversePts >= 30:
				item.Reason = "wrong_direction"
			case source.mode == "pullback":
				item.Reason = "failed_pullback_resume"
			default:
				item.Reason = "unclassified_no_followthrough"
			}
			report.Trades = append(report.Trades, item)
			report.ModeCounts[item.Mode]++
			report.ReasonCounts[item.Reason]++
		}
	}
	return report, nil
}

func matchingEntry(entries []entryDiagnostic, mode string, timeMS int64) *entryDiagnostic {
	for index := range entries {
		if entries[index].Mode == mode && entries[index].TimeMS == timeMS {
			entry := entries[index]
			return &entry
		}
	}
	return nil
}

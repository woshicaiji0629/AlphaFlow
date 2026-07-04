package simulator

import (
	"fmt"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

type openTradeFill struct {
	event         strategy.StrategyEvent
	remainingSize float64
}

func BuildBacktestTrades(events []strategy.StrategyEvent) ([]strategy.BacktestTrade, error) {
	openFills := map[string][]openTradeFill{}
	trades := []strategy.BacktestTrade{}
	for _, event := range events {
		if event.EventType != strategy.EventTypeOrderFilled {
			continue
		}
		key := tradePairingKey(event)
		if isExitFill(event) {
			nextTrades, err := closeOpenFills(openFills, key, event)
			if err != nil {
				return nil, err
			}
			trades = append(trades, nextTrades...)
			continue
		}
		if event.Size <= 0 {
			continue
		}
		openFills[key] = append(openFills[key], openTradeFill{
			event:         event,
			remainingSize: event.Size,
		})
	}
	return trades, nil
}

func closeOpenFills(openFills map[string][]openTradeFill, key string, exitEvent strategy.StrategyEvent) ([]strategy.BacktestTrade, error) {
	if exitEvent.Size <= 0 {
		return nil, nil
	}
	queue := openFills[key]
	if len(queue) == 0 {
		return nil, fmt.Errorf("backtest trade exit has no matching entry event_id=%s", exitEvent.EventID)
	}
	remainingExit := exitEvent.Size
	trades := []strategy.BacktestTrade{}
	for remainingExit > 0 {
		if len(queue) == 0 {
			return nil, fmt.Errorf("backtest trade exit exceeds matching entry size event_id=%s", exitEvent.EventID)
		}
		entry := queue[0]
		size := minFloat(entry.remainingSize, remainingExit)
		trades = append(trades, buildTrade(entry.event, exitEvent, size))
		entry.remainingSize -= size
		remainingExit -= size
		if entry.remainingSize <= 0 {
			queue = queue[1:]
		} else {
			queue[0] = entry
		}
	}
	openFills[key] = queue
	return trades, nil
}

func buildTrade(entryEvent strategy.StrategyEvent, exitEvent strategy.StrategyEvent, size float64) strategy.BacktestTrade {
	return strategy.BacktestTrade{
		TradeID:              tradeID(entryEvent, exitEvent, size),
		RunID:                exitEvent.RunID,
		Account:              exitEvent.Account,
		Exchange:             exitEvent.Exchange,
		Market:               exitEvent.Market,
		Symbol:               exitEvent.Symbol,
		StrategyName:         exitEvent.StrategyName,
		PositionSide:         exitEvent.PositionSide,
		EntryTime:            entryEvent.EventTime,
		EntryBarOpenTime:     entryEvent.BarOpenTime,
		EntryPrice:           entryEvent.Price,
		EntrySize:            size,
		EntryReason:          entryEvent.Reason,
		ExitTime:             exitEvent.EventTime,
		ExitBarOpenTime:      exitEvent.BarOpenTime,
		ExitPrice:            exitEvent.Price,
		ExitSize:             size,
		ExitReason:           exitEvent.Metadata["exit_reason"],
		PnL:                  exitEvent.PnL,
		Fee:                  exitEvent.Fee,
		ReturnPct:            exitEvent.Metadata["return_pct"],
		ReturnOnMarginPct:    exitEvent.Metadata["return_on_margin_pct"],
		EntryEventID:         entryEvent.EventID,
		ExitEventID:          exitEvent.EventID,
		EntryExchangeOrderID: entryEvent.ExchangeOrderID,
		ExitExchangeOrderID:  exitEvent.ExchangeOrderID,
		Metadata:             tradeMetadata(exitEvent),
		CreatedAt:            exitEvent.CreatedAt,
	}
}

func tradeID(entryEvent strategy.StrategyEvent, exitEvent strategy.StrategyEvent, size float64) string {
	return strings.Join([]string{
		exitEvent.RunID,
		exitEvent.Exchange,
		exitEvent.Market,
		exitEvent.Symbol,
		exitEvent.StrategyName,
		string(exitEvent.PositionSide),
		entryEvent.EventID,
		exitEvent.EventID,
		formatSummaryFloat(size),
	}, ":")
}

func tradePairingKey(event strategy.StrategyEvent) string {
	return strings.Join([]string{
		event.RunID,
		event.Exchange,
		event.Market,
		event.Symbol,
		event.StrategyName,
		string(event.PositionSide),
	}, ":")
}

func tradeMetadata(exitEvent strategy.StrategyEvent) map[string]string {
	metadata := map[string]string{}
	for _, key := range []string{
		"gross_pnl",
		"gross_fee",
		"rebate",
		"fee_rate",
		"rebate_pct",
		"margin_quote",
		"leverage",
		"rule_reason",
		"trigger_price",
	} {
		if value := exitEvent.Metadata[key]; value != "" {
			metadata[key] = value
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func minFloat(left float64, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

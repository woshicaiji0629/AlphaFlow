package simulator

import (
	"encoding/json"
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
		Metadata:             tradeMetadata(entryEvent, exitEvent),
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

func tradeMetadata(entryEvent strategy.StrategyEvent, exitEvent strategy.StrategyEvent) map[string]string {
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
		"mfe_price",
		"mae_price",
		"mfe_bps",
		"mae_bps",
		"mfe_bar_open_time_ms",
		"mae_bar_open_time_ms",
		"exit_move_bps",
		"profit_giveback_bps",
		"holding_time_ms",
	} {
		if value := exitEvent.Metadata[key]; value != "" {
			metadata[key] = value
		}
	}
	for key, value := range entryMetadata(entryEvent) {
		metadata[key] = value
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func entryMetadata(entryEvent strategy.StrategyEvent) map[string]string {
	metadata := map[string]string{}
	if entryEvent.Score > 0 {
		metadata["entry_score"] = formatSummaryFloat(entryEvent.Score)
	}
	if entryEvent.Confidence > 0 {
		metadata["entry_confidence"] = formatSummaryFloat(entryEvent.Confidence)
	}
	payload := entryEvent.Metadata["analysis"]
	if payload == "" {
		return metadata
	}
	var analysis strategy.Analysis
	if err := json.Unmarshal([]byte(payload), &analysis); err != nil {
		metadata["entry_analysis_error"] = err.Error()
		return metadata
	}
	for _, check := range analysis.Checks {
		if check.Side != "" && check.Side != entryEvent.Side {
			continue
		}
		switch check.Name {
		case "entry_mode":
			copyDiagnosticValue(metadata, check.Values, "mode", "entry_mode")
			copyDiagnosticValue(metadata, check.Values, "trigger_sources", "trigger_source")
			copyDiagnosticValue(metadata, check.Values, "trigger_source_count", "trigger_source_count")
			copyDiagnosticValue(metadata, check.Values, "ma_tangled", "entry_ma_tangled")
			copyDiagnosticValue(metadata, check.Values, "volatility_state", "entry_volatility_state")
			copyDiagnosticValue(metadata, check.Values, "local_supertrend_direction", "entry_supertrend_direction")
			copyDiagnosticValue(metadata, check.Values, "local_trend_bias", "entry_trend_bias")
			copyDiagnosticValue(metadata, check.Values, "local_ma_bias", "entry_ma_bias")
			copyDiagnosticValue(metadata, check.Values, "local_macd_bias", "entry_macd_bias")
		case "higher_timeframe_regime":
			copyDiagnosticValue(metadata, check.Values, "state", "entry_regime_state")
			copyDiagnosticValue(metadata, check.Values, "10m", "entry_10m_state")
			copyDiagnosticValue(metadata, check.Values, "15m", "entry_15m_state")
			copyDiagnosticValue(metadata, check.Values, "30m", "entry_30m_state")
			copyDiagnosticValue(metadata, check.Values, "10m_stable", "entry_10m_stable")
		case "pullback_resolution":
			copyDiagnosticValue(metadata, check.Values, "5m", "entry_5m_state")
		case "fake_signal_risk":
			copyDiagnosticValue(metadata, check.Values, "risk", "entry_fake_risk")
		case "momentum_energy":
			copyDiagnosticValue(metadata, check.Values, "confirmations", "entry_momentum_confirmations")
			copyDiagnosticValue(metadata, check.Values, "ma", "entry_momentum_ma")
			copyDiagnosticValue(metadata, check.Values, "macd", "entry_momentum_macd")
			copyDiagnosticValue(metadata, check.Values, "price_volume", "entry_momentum_price_volume")
			copyDiagnosticValue(metadata, check.Values, "volume_expansion", "entry_momentum_volume_expansion")
		case "stc":
			copyDiagnosticValue(metadata, check.Values, "value", "entry_stc")
			copyDiagnosticValue(metadata, check.Values, "previous", "entry_stc_previous")
			copyDiagnosticValue(metadata, check.Values, "delta", "entry_stc_delta")
			copyDiagnosticValue(metadata, check.Values, "direction", "entry_stc_direction")
			copyDiagnosticValue(metadata, check.Values, "zone", "entry_stc_zone")
			copyDiagnosticValue(metadata, check.Values, "cross", "entry_stc_cross")
			copyDiagnosticValue(metadata, check.Values, "entry_veto", "entry_stc_veto")
		case "entry_feature_snapshot":
			for _, key := range []string{
				"market_direction_score",
				"market_direction_agreement",
				"market_direction_conflict",
				"market_trend_strength",
				"market_momentum_strength",
				"market_volatility_health",
				"market_structure_quality",
				"market_volume_confirmation",
				"market_location_score",
				"market_risk_score",
				"market_data_confidence",
				"market_score_available_features",
				"market_score_expected_features",
				"market_strength_score",
				"market_risk_adjusted_strength_score",
				"market_directional_capability_score",
				"market_score_version",
				"market_direction_bias",
				"market_strength_state",
			} {
				copyDiagnosticValue(metadata, check.Values, key, "entry_"+key)
			}
		}
	}
	return metadata
}

func copyDiagnosticValue(target map[string]string, values map[string]string, sourceKey string, targetKey string) {
	if value := values[sourceKey]; value != "" {
		target[targetKey] = value
	}
}

func minFloat(left float64, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

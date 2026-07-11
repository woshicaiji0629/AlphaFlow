package app

import (
	"strings"
	"time"

	"alphaflow/go-service/market-data/internal/aggregator"
	"alphaflow/go-service/market-data/internal/collector"
	"alphaflow/go-service/market-data/internal/config"
	"alphaflow/go-service/market-data/internal/exchange/binance"
	"alphaflow/go-service/market-data/internal/exchange/bitget"
	"alphaflow/go-service/market-data/internal/exchange/bybit"
	"alphaflow/go-service/market-data/internal/exchange/gate"
	"alphaflow/go-service/market-data/internal/store"
	"alphaflow/go-service/pkg/httpclient"
)

func buildCollectors(
	cfg config.Config,
	marketStore *store.MarketStore,
	reconnectDelay time.Duration,
	gapPublisher collector.GapPublisher,
) []*collector.Collector {
	httpClient := httpclient.New()
	collectors := []*collector.Collector{}
	if cfg.Binance.Enabled {
		collectors = append(collectors, collector.New(
			collector.Options{
				Symbols:              cfg.Binance.Symbols,
				Intervals:            config.BinanceIntervals(),
				RESTLimit:            config.RESTLimit(),
				ReconnectDelay:       reconnectDelay,
				LiquidationLimit:     config.LiquidationLimit(),
				PollOpenInterest:     true,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
				WebSocketConnections: cfg.Binance.WebSocketConnections,
				StartupLookback:      config.KlineLimit(),
				StartupDerivedRules:  rulesForExchange(aggregationRules(cfg), "binance"),
				GapPublisher:         gapPublisher,
			},
			binance.NewRESTClient(config.BinanceRESTBase(), httpClient),
			binance.NewWSClient(config.BinanceWSBase()),
			marketStore,
		))
	}
	if cfg.Gate.Enabled {
		gateIntervals := config.GateIntervals()
		collectors = append(collectors, collector.New(
			collector.Options{
				Symbols:              cfg.Gate.Symbols,
				Intervals:            gateIntervals,
				RESTLimit:            config.RESTLimit(),
				ReconnectDelay:       reconnectDelay,
				LiquidationLimit:     config.LiquidationLimit(),
				PollOpenInterest:     false,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
				WebSocketConnections: cfg.Gate.WebSocketConnections,
				StartupLookback:      config.KlineLimit(),
				StartupDerivedRules:  rulesForExchange(aggregationRules(cfg), "gate"),
				GapPublisher:         gapPublisher,
			},
			gate.NewRESTClient(config.GateRESTBase(), config.GateSettle(), httpClient),
			gate.NewWSClient(config.GateWSBase(), config.GateSettle(), gateIntervals[0]),
			marketStore,
		))
	}
	if cfg.Bitget.Enabled {
		bitgetBackfillIntervals := withExtraIntervals(config.BitgetIntervals(), "3m")
		collectors = append(collectors, collector.New(
			collector.Options{
				Symbols:              cfg.Bitget.Symbols,
				Intervals:            config.BitgetIntervals(),
				RESTLimit:            config.RESTLimit(),
				ReconnectDelay:       reconnectDelay,
				LiquidationLimit:     config.LiquidationLimit(),
				PollOpenInterest:     false,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
				WebSocketConnections: cfg.Bitget.WebSocketConnections,
				StartupLookback:      config.KlineLimit(),
				BackfillIntervals:    bitgetBackfillIntervals,
				StartupDerivedRules:  rulesExceptTarget(aggregationRules(cfg), "bitget", "3m"),
				GapPublisher:         gapPublisher,
			},
			bitget.NewRESTClient(config.BitgetRESTBase(), config.BitgetProductType(), httpClient),
			bitget.NewWSClient(config.BitgetWSBase(), config.BitgetProductType()),
			marketStore,
		))
	}
	if cfg.Bybit.Enabled {
		collectors = append(collectors, collector.New(
			collector.Options{
				Symbols:              cfg.Bybit.Symbols,
				Intervals:            config.BybitIntervals(),
				RESTLimit:            config.RESTLimit(),
				ReconnectDelay:       reconnectDelay,
				LiquidationLimit:     config.LiquidationLimit(),
				PollOpenInterest:     false,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
				WebSocketConnections: cfg.Bybit.WebSocketConnections,
				StartupLookback:      config.KlineLimit(),
				StartupDerivedRules:  rulesForExchange(aggregationRules(cfg), "bybit"),
				GapPublisher:         gapPublisher,
			},
			bybit.NewRESTClient(config.BybitRESTBase(), config.BybitCategory(), httpClient),
			bybit.NewWSClient(config.BybitWSBase(), config.BybitCategory()),
			marketStore,
		))
	}
	return collectors
}

func rulesForExchange(rules []aggregator.Rule, exchange string) []aggregator.Rule {
	result := make([]aggregator.Rule, 0, len(rules))
	for _, rule := range rules {
		if strings.EqualFold(rule.Exchange, exchange) {
			result = append(result, rule)
		}
	}
	return result
}

func rulesExceptTarget(rules []aggregator.Rule, exchange string, target string) []aggregator.Rule {
	result := rulesForExchange(rules, exchange)
	filtered := result[:0]
	for _, rule := range result {
		if rule.TargetInterval != target {
			filtered = append(filtered, rule)
		}
	}
	return filtered
}

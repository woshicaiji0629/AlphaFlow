package app

import (
	"time"

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
			},
			gate.NewRESTClient(config.GateRESTBase(), config.GateSettle(), httpClient),
			gate.NewWSClient(config.GateWSBase(), config.GateSettle(), gateIntervals[0]),
			marketStore,
		))
	}
	if cfg.Bitget.Enabled {
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
			},
			bybit.NewRESTClient(config.BybitRESTBase(), config.BybitCategory(), httpClient),
			bybit.NewWSClient(config.BybitWSBase(), config.BybitCategory()),
			marketStore,
		))
	}
	return collectors
}

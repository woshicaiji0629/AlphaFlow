package model

import "testing"

func TestMarketDataKeys(t *testing.T) {
	tests := map[string]string{
		BookTickerKey("binance", "um", "ETHUSDT"):      "bn:um:bt:ETHUSDT",
		LiquidationKey("binance", "um", "ETHUSDT"):     "bn:um:liq:ETHUSDT",
		OpenInterestKey("binance", "um", "ETHUSDT"):    "bn:um:oi:ETHUSDT",
		MarketStatusKey("binance", "um"):               "bn:um:status",
		WebSocketStatusKey("binance", "um"):            "bn:um:ws",
		WebSocketShardStatusKey("binance", "um", "0"):  "bn:um:ws:0",
		IndicatorKey("binance", "um", "ETHUSDT", "3m"): "bn:um:ind:ETHUSDT:3m",
	}

	for got, want := range tests {
		if got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}

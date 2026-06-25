package model

import "testing"

func TestMarketDataKeys(t *testing.T) {
	tests := map[string]string{
		BookTickerKey("binance", "um", "ETHUSDT"):      "bn:um:bt:ETHUSDT",
		LiquidationKey("binance", "um", "ETHUSDT"):     "bn:um:liq:ETHUSDT",
		OpenInterestKey("binance", "um", "ETHUSDT"):    "bn:um:oi:ETHUSDT",
		MarketStatusKey("binance", "um"):               "bn:um:status",
		IndicatorKey("binance", "um", "ETHUSDT", "3m"): "bn:um:ind:ETHUSDT:3m",
	}

	for got, want := range tests {
		if got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}

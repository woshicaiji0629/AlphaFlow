package model

import "testing"

func TestMarketDataKeys(t *testing.T) {
	tests := map[string]string{
		BookTickerKey("binance", "um", "ETHUSDT"):   "bn:um:bt:ETHUSDT",
		LiquidationKey("binance", "um", "ETHUSDT"):  "bn:um:liq:ETHUSDT",
		OpenInterestKey("binance", "um", "ETHUSDT"): "bn:um:oi:ETHUSDT",
	}

	for got, want := range tests {
		if got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}

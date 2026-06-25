package model

import "testing"

func TestPriceKeys(t *testing.T) {
	tests := map[string]string{
		LastPriceKey("binance", "um", "ETHUSDT"): "bn:um:lp:ETHUSDT",
		MarkPriceKey("binance", "um", "ETHUSDT"): "bn:um:mp:ETHUSDT",
	}

	for got, want := range tests {
		if got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}

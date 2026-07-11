package config

import "testing"

func TestSupportedSymbol(t *testing.T) {
	for _, symbol := range []string{"BTC", "ETH", "SOL", "XRP", "DOGE", "BNB", "HYPE"} {
		if !supportedSymbol(symbol) {
			t.Errorf("expected %s to be supported", symbol)
		}
	}
	if supportedSymbol("ADA") {
		t.Fatal("ADA should not be supported before a verified market mapping is added")
	}
}

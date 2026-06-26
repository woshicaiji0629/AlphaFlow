package model

import "testing"

func TestDataHealthKey(t *testing.T) {
	got := DataHealthKey("binance", "um", "ETHUSDT", "1m")
	want := "bn:um:health:ETHUSDT:1m"
	if got != want {
		t.Fatalf("DataHealthKey() = %q, want %q", got, want)
	}
}

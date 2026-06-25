package okx

import "testing"

func TestParseKline(t *testing.T) {
	kline, err := parseKline("ETH-USDT-SWAP", "1m", []string{
		"1700000000000", "1", "2", "0.5", "1.5", "10", "20", "30", "1",
	})
	if err != nil {
		t.Fatalf("parseKline: %v", err)
	}
	if kline.Exchange != "okx" {
		t.Fatalf("exchange = %q, want okx", kline.Exchange)
	}
	if kline.Market != "swap" {
		t.Fatalf("market = %q, want swap", kline.Market)
	}
	if !kline.IsClosed {
		t.Fatal("expected closed kline")
	}
}

func TestOKXInterval(t *testing.T) {
	if got := okxInterval("1h"); got != "1H" {
		t.Fatalf("okxInterval(1h) = %q, want 1H", got)
	}
}

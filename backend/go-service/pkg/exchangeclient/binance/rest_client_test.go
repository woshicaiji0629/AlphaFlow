package binance

import "testing"

func TestParseRESTKline(t *testing.T) {
	kline, err := parseRESTKline("ETHUSDT", "1m", []any{
		float64(1700000000000),
		"1",
		"2",
		"0.5",
		"1.5",
		"10",
		float64(1700000059999),
		"15",
		float64(123),
		"4",
		"6",
	})
	if err != nil {
		t.Fatalf("parseRESTKline: %v", err)
	}
	if kline.Exchange != "binance" || kline.Market != "um" {
		t.Fatalf("identity = %#v", kline)
	}
	if kline.OpenTime != 1700000000000 || kline.CloseTime != 1700000059999 {
		t.Fatalf("time = %#v", kline)
	}
	if kline.TradeCount != 123 || !kline.IsClosed {
		t.Fatalf("trade/closed = %#v", kline)
	}
}

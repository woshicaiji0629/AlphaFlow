package bitget

import "testing"

func TestKlineFromRaw(t *testing.T) {
	client := NewRESTClient("https://example.test", "USDT-FUTURES", nil)

	kline, err := client.klineFromRaw("ETHUSDT", "1m", []string{
		"1700000000000", "1", "2", "0.5", "1.5", "10", "15",
	})
	if err != nil {
		t.Fatalf("klineFromRaw: %v", err)
	}
	if kline.Exchange != "bitget" {
		t.Fatalf("exchange = %q, want bitget", kline.Exchange)
	}
	if kline.Market != "usdt-futures" {
		t.Fatalf("market = %q, want usdt-futures", kline.Market)
	}
	if kline.OpenTime != 1700000000000 {
		t.Fatalf("open time = %d, want 1700000000000", kline.OpenTime)
	}
	if kline.QuoteVolume != "15" {
		t.Fatalf("quote volume = %q, want 15", kline.QuoteVolume)
	}
}

func TestBitgetInterval(t *testing.T) {
	if got := bitgetInterval("1h"); got != "1H" {
		t.Fatalf("bitgetInterval(1h) = %q, want 1H", got)
	}
	if got := bitgetInterval("5m"); got != "5m" {
		t.Fatalf("bitgetInterval(5m) = %q, want 5m", got)
	}
}

package bybit

import "testing"

func TestKlineFromRaw(t *testing.T) {
	client := NewRESTClient("https://example.test", "linear", nil)

	kline, err := client.klineFromRaw("ETHUSDT", "1m", []string{
		"1700000000000", "1", "2", "0.5", "1.5", "10", "15",
	})
	if err != nil {
		t.Fatalf("klineFromRaw: %v", err)
	}
	if kline.Exchange != "bybit" {
		t.Fatalf("exchange = %q, want bybit", kline.Exchange)
	}
	if kline.Market != "linear" {
		t.Fatalf("market = %q, want linear", kline.Market)
	}
	if kline.OpenTime != 1700000000000 {
		t.Fatalf("open time = %d, want 1700000000000", kline.OpenTime)
	}
	if kline.QuoteVolume != "15" {
		t.Fatalf("quote volume = %q, want 15", kline.QuoteVolume)
	}
}

func TestBybitInterval(t *testing.T) {
	if got := bybitInterval("1h"); got != "60" {
		t.Fatalf("bybitInterval(1h) = %q, want 60", got)
	}
	if got := bybitInterval("5m"); got != "5" {
		t.Fatalf("bybitInterval(5m) = %q, want 5", got)
	}
}

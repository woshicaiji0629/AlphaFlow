package gate

import "testing"

func TestKlineFromREST(t *testing.T) {
	client := NewRESTClient("https://example.test", "usdt", nil)

	kline, err := client.klineFromREST("ETH_USDT", "1m", restKline{
		Time:        1700000000,
		Open:        "1",
		High:        "2",
		Low:         "0.5",
		Close:       "1.5",
		Volume:      "10",
		QuoteVolume: "15",
	})
	if err != nil {
		t.Fatalf("klineFromREST: %v", err)
	}
	if kline.Exchange != "gate" {
		t.Fatalf("exchange = %q, want gate", kline.Exchange)
	}
	if kline.Market != "usdt" {
		t.Fatalf("market = %q, want usdt", kline.Market)
	}
	if kline.OpenTime != 1700000000000 {
		t.Fatalf("open time = %d, want 1700000000000", kline.OpenTime)
	}
	if !kline.IsClosed {
		t.Fatal("expected REST kline to be closed")
	}
}

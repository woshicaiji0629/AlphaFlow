package gate

import (
	"encoding/json"
	"testing"
)

func TestKlineFromREST(t *testing.T) {
	client := NewRESTClient("https://example.test", "usdt", nil)

	kline, err := client.klineFromREST("ETH_USDT", "1m", restKline{
		Time:        1700000000,
		Open:        flexString("1"),
		High:        flexString("2"),
		Low:         flexString("0.5"),
		Close:       flexString("1.5"),
		Volume:      flexString("10"),
		QuoteVolume: flexString("15"),
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

func TestDecodeRESTKlineWithNumericFields(t *testing.T) {
	raw := []byte(`[{"t":1700000000,"o":1,"h":2,"l":0.5,"c":1.5,"v":10,"sum":15}]`)

	var klines []restKline
	if err := json.Unmarshal(raw, &klines); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	client := NewRESTClient("https://example.test", "usdt", nil)
	kline, err := client.klineFromREST("ETH_USDT", "1m", klines[0])
	if err != nil {
		t.Fatalf("klineFromREST: %v", err)
	}
	if kline.Volume != "10" {
		t.Fatalf("volume = %q, want 10", kline.Volume)
	}
	if kline.QuoteVolume != "15" {
		t.Fatalf("quote volume = %q, want 15", kline.QuoteVolume)
	}
}

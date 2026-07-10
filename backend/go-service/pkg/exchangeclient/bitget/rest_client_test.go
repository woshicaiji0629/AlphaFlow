package bitget

import (
	"context"
	"testing"
)

type fakeHTTPClient struct {
	body []byte
}

func (c fakeHTTPClient) Get(context.Context, string, map[string]string) ([]byte, error) {
	return c.body, nil
}

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

func TestFetchKlinesExcludesCurrentCandle(t *testing.T) {
	client := NewRESTClient("https://example.test", "USDT-FUTURES", fakeHTTPClient{body: []byte(`{
		"code":"00000",
		"requestTime":1700000060000,
		"data":[
			["1700000040000","1","2","0.5","1.5","10","15"],
			["1699999980000","1","2","0.5","1.5","10","15"]
		]
	}`)})
	klines, err := client.FetchKlines(context.Background(), "ETHUSDT", "1m", 100, 0)
	if err != nil {
		t.Fatalf("FetchKlines: %v", err)
	}
	if len(klines) != 1 || klines[0].OpenTime != 1699999980000 || !klines[0].IsClosed {
		t.Fatalf("klines = %#v, want only closed candle", klines)
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

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

func TestIntervalFromChannel(t *testing.T) {
	got, ok := intervalFromChannel("candle4H")
	if !ok {
		t.Fatal("expected candle channel to parse")
	}
	if got != "4h" {
		t.Fatalf("interval = %q, want 4h", got)
	}
}

func TestTickerConversions(t *testing.T) {
	client := NewWSClient("wss://example.test", "USDT-FUTURES")
	ticker := wsTicker{
		InstID:       "ETHUSDT",
		LastPrice:    "2500",
		MarkPrice:    "2499",
		IndexPrice:   "2501",
		FundingRate:  "0.0001",
		NextFunding:  1700007200000,
		BidPrice:     "2498",
		BidQuantity:  "2",
		AskPrice:     "2502",
		AskQuantity:  "3",
		OpenInterest: "100",
	}

	lastPrice := client.lastPriceFromTicker(1700000000000, ticker)
	if lastPrice.Price != "2500" || lastPrice.Market != "usdt-futures" {
		t.Fatalf("last price = %#v", lastPrice)
	}
	markPrice := client.markPriceFromTicker(1700000000000, ticker)
	if markPrice.MarkPrice != "2499" || markPrice.NextFundingTime != 1700007200000 {
		t.Fatalf("mark price = %#v", markPrice)
	}
	bookTicker := client.bookTickerFromTicker(1700000000000, ticker)
	if bookTicker.BidPrice != "2498" || bookTicker.AskPrice != "2502" {
		t.Fatalf("book ticker = %#v", bookTicker)
	}
	openInterest := client.openInterestFromTicker(1700000000000, ticker, "100")
	if openInterest.OpenInterest != "100" {
		t.Fatalf("open interest = %#v", openInterest)
	}
}

func TestTradeConversion(t *testing.T) {
	client := NewWSClient("wss://example.test", "USDT-FUTURES")

	lastPrice := client.lastPriceFromTrade(1700000000000, "ETHUSDT", wsTrade{
		TradeID: "123",
		Price:   "2500",
		Size:    "1.5",
	})
	if lastPrice.Symbol != "ETHUSDT" || lastPrice.Price != "2500" || lastPrice.Quantity != "1.5" {
		t.Fatalf("last price = %#v", lastPrice)
	}
	if lastPrice.TradeID != 123 {
		t.Fatalf("trade id = %d, want 123", lastPrice.TradeID)
	}
}

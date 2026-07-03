package bybit

import "testing"

func TestParseTopic(t *testing.T) {
	symbol, interval, ok := parseTopic("kline.240.ETHUSDT")
	if !ok {
		t.Fatal("expected topic to parse")
	}
	if symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", symbol)
	}
	if interval != "4h" {
		t.Fatalf("interval = %q, want 4h", interval)
	}
}

func TestTickerConversions(t *testing.T) {
	client := NewWSClient("wss://example.test", "linear")
	ticker := wsTicker{
		Symbol:          "ETHUSDT",
		LastPrice:       "2500",
		MarkPrice:       "2499",
		IndexPrice:      "2501",
		OpenInterest:    "100",
		BidPrice:        "2498",
		BidQuantity:     "2",
		AskPrice:        "2502",
		AskQuantity:     "3",
		FundingRate:     "0.0001",
		NextFundingTime: "1700007200000",
		CrossSeq:        99,
	}

	lastPrice := client.lastPriceFromTicker(1700000000000, ticker)
	if lastPrice.Price != "2500" || lastPrice.Market != "linear" {
		t.Fatalf("last price = %#v", lastPrice)
	}
	markPrice := client.markPriceFromTicker(1700000000000, ticker)
	if markPrice.MarkPrice != "2499" || markPrice.NextFundingTime != 1700007200000 {
		t.Fatalf("mark price = %#v", markPrice)
	}
	bookTicker := client.bookTickerFromTicker(1700000000000, ticker)
	if bookTicker.BidPrice != "2498" || bookTicker.AskPrice != "2502" || bookTicker.UpdateID != 99 {
		t.Fatalf("book ticker = %#v", bookTicker)
	}
	openInterest := client.openInterestFromTicker(1700000000000, ticker)
	if openInterest.OpenInterest != "100" {
		t.Fatalf("open interest = %#v", openInterest)
	}
}

func TestTradeConversion(t *testing.T) {
	client := NewWSClient("wss://example.test", "linear")

	lastPrice := client.lastPriceFromTrade(0, wsTrade{
		TradeTime: 1700000000000,
		Symbol:    "ETHUSDT",
		Size:      "1.5",
		Price:     "2500",
		TradeID:   "123",
	})
	if lastPrice.Symbol != "ETHUSDT" || lastPrice.Price != "2500" || lastPrice.Quantity != "1.5" {
		t.Fatalf("last price = %#v", lastPrice)
	}
	if lastPrice.EventTime != 1700000000000 || lastPrice.TradeID != 123 {
		t.Fatalf("last price timing/id = %#v", lastPrice)
	}
}

func TestLiquidationConversion(t *testing.T) {
	client := NewWSClient("wss://example.test", "linear")

	liquidation := client.liquidationFromWS(0, wsLiquidation{
		TradeTime: 1700000000000,
		Symbol:    "ETHUSDT",
		Side:      "Sell",
		Size:      "2",
		Price:     "2400",
	})
	if liquidation.Symbol != "ETHUSDT" || liquidation.Side != "Sell" {
		t.Fatalf("liquidation = %#v", liquidation)
	}
	if liquidation.TradeTime != 1700000000000 || liquidation.AccumulatedQty != "2" {
		t.Fatalf("liquidation timing/qty = %#v", liquidation)
	}
}

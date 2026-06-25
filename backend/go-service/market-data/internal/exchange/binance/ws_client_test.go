package binance

import (
	"context"
	"encoding/json"
	"testing"

	"alphaflow/go-service/market-data/internal/model"
)

type fakeHandler struct {
	lastPrice   model.LastPrice
	markPrice   model.MarkPrice
	bookTicker  model.BookTicker
	liquidation model.Liquidation
}

func (h *fakeHandler) HandleKline(context.Context, model.Kline) error {
	return nil
}

func (h *fakeHandler) HandleLastPrice(_ context.Context, price model.LastPrice) error {
	h.lastPrice = price
	return nil
}

func (h *fakeHandler) HandleMarkPrice(_ context.Context, price model.MarkPrice) error {
	h.markPrice = price
	return nil
}

func (h *fakeHandler) HandleBookTicker(_ context.Context, ticker model.BookTicker) error {
	h.bookTicker = ticker
	return nil
}

func (h *fakeHandler) HandleOpenInterest(context.Context, model.OpenInterest) error {
	return nil
}

func (h *fakeHandler) HandleLiquidation(_ context.Context, liquidation model.Liquidation) error {
	h.liquidation = liquidation
	return nil
}

func TestDispatchAggTrade(t *testing.T) {
	raw := json.RawMessage(`{
		"stream":"ethusdt@aggTrade",
		"data":{
			"e":"aggTrade",
			"E":123456789,
			"s":"ETHUSDT",
			"a":5933014,
			"p":"3500.12",
			"q":"1.5",
			"T":123456780
		}
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if handler.lastPrice.Symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", handler.lastPrice.Symbol)
	}
	if handler.lastPrice.Price != "3500.12" {
		t.Fatalf("price = %q, want 3500.12", handler.lastPrice.Price)
	}
	if handler.lastPrice.TradeID != 5933014 {
		t.Fatalf("trade id = %d, want 5933014", handler.lastPrice.TradeID)
	}
}

func TestDispatchMarkPrice(t *testing.T) {
	raw := json.RawMessage(`{
		"stream":"ethusdt@markPrice@1s",
		"data":{
			"e":"markPriceUpdate",
			"E":123456789,
			"s":"ETHUSDT",
			"p":"3501.10",
			"i":"3500.90",
			"r":"0.0001",
			"T":123999999
		}
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if handler.markPrice.Symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", handler.markPrice.Symbol)
	}
	if handler.markPrice.MarkPrice != "3501.10" {
		t.Fatalf("mark price = %q, want 3501.10", handler.markPrice.MarkPrice)
	}
	if handler.markPrice.IndexPrice != "3500.90" {
		t.Fatalf("index price = %q, want 3500.90", handler.markPrice.IndexPrice)
	}
}

func TestDispatchBookTicker(t *testing.T) {
	raw := json.RawMessage(`{
		"stream":"ethusdt@bookTicker",
		"data":{
			"e":"bookTicker",
			"u":400900217,
			"s":"ETHUSDT",
			"E":1568014460893,
			"T":1568014460891,
			"b":"3500.10",
			"B":"2.5",
			"a":"3500.20",
			"A":"3.1"
		}
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if handler.bookTicker.Symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", handler.bookTicker.Symbol)
	}
	if handler.bookTicker.BidPrice != "3500.10" {
		t.Fatalf("bid price = %q, want 3500.10", handler.bookTicker.BidPrice)
	}
	if handler.bookTicker.AskPrice != "3500.20" {
		t.Fatalf("ask price = %q, want 3500.20", handler.bookTicker.AskPrice)
	}
}

func TestDispatchForceOrder(t *testing.T) {
	raw := json.RawMessage(`{
		"stream":"ethusdt@forceOrder",
		"data":{
			"e":"forceOrder",
			"E":1568014460893,
			"o":{
				"s":"ETHUSDT",
				"S":"SELL",
				"o":"LIMIT",
				"f":"IOC",
				"q":"0.014",
				"p":"3500.00",
				"ap":"3499.50",
				"X":"FILLED",
				"l":"0.014",
				"z":"0.014",
				"T":1568014460893
			}
		}
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if handler.liquidation.Symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", handler.liquidation.Symbol)
	}
	if handler.liquidation.Side != "SELL" {
		t.Fatalf("side = %q, want SELL", handler.liquidation.Side)
	}
	if handler.liquidation.AveragePrice != "3499.50" {
		t.Fatalf("average price = %q, want 3499.50", handler.liquidation.AveragePrice)
	}
}

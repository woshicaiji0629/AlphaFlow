package gate

import (
	"context"
	"encoding/json"
	"testing"

	"alphaflow/go-service/market-data/internal/exchange"
	"alphaflow/go-service/market-data/internal/model"
)

type fakeHandler struct {
	lastPrice    model.LastPrice
	markPrice    model.MarkPrice
	bookTicker   model.BookTicker
	openInterest model.OpenInterest
	liquidation  model.Liquidation
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

func (h *fakeHandler) HandleOpenInterest(_ context.Context, interest model.OpenInterest) error {
	h.openInterest = interest
	return nil
}

func (h *fakeHandler) HandleLiquidation(_ context.Context, liquidation model.Liquidation) error {
	h.liquidation = liquidation
	return nil
}

func TestKlineFromWS(t *testing.T) {
	client := NewWSClient("wss://example.test", "usdt", "1m")

	kline, err := client.klineFromWS(1700000000123, wsKline{
		Time:        1700000000,
		Open:        "1",
		High:        "2",
		Low:         "0.5",
		Close:       "1.5",
		Volume:      "10",
		Name:        "1m_ETH_USDT",
		QuoteVolume: "15",
		IsClosed:    true,
	})
	if err != nil {
		t.Fatalf("klineFromWS: %v", err)
	}
	if kline.Symbol != "ETH_USDT" {
		t.Fatalf("symbol = %q, want ETH_USDT", kline.Symbol)
	}
	if kline.Interval != "1m" {
		t.Fatalf("interval = %q, want 1m", kline.Interval)
	}
	if kline.EventTime != 1700000000123 {
		t.Fatalf("event time = %d, want 1700000000123", kline.EventTime)
	}
}

func TestSubscribeMessages(t *testing.T) {
	client := NewWSClient("wss://example.test", "usdt", "1m")

	tests := map[exchange.StreamType]string{
		exchange.StreamTypeAggTrade:   "futures.trades",
		exchange.StreamTypeBookTicker: "futures.book_ticker",
		exchange.StreamTypeMarkPrice:  "futures.contract_stats",
		exchange.StreamTypeForceOrder: "futures.public_liquidates",
	}
	for streamType, channel := range tests {
		messages := client.subscribeMessages(exchange.Stream{
			Symbol: "ETH_USDT",
			Type:   streamType,
		})
		if len(messages) != 1 {
			t.Fatalf("%s messages length = %d, want 1", streamType, len(messages))
		}
		if messages[0].Channel != channel {
			t.Fatalf("%s channel = %q, want %q", streamType, messages[0].Channel, channel)
		}
	}
}

func TestDispatchTrade(t *testing.T) {
	raw := json.RawMessage(`{
		"time_ms": 1541503698123,
		"channel": "futures.trades",
		"event": "update",
		"result": [{
			"size": "-108",
			"id": 27753479,
			"create_time_ms": 1545136464123,
			"price": "96.4",
			"contract": "ETH_USDT"
		}]
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "usdt", "1m")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handler.lastPrice.Symbol != "ETH_USDT" {
		t.Fatalf("symbol = %q, want ETH_USDT", handler.lastPrice.Symbol)
	}
	if handler.lastPrice.Quantity != "-108" {
		t.Fatalf("quantity = %q, want -108", handler.lastPrice.Quantity)
	}
}

func TestDispatchBookTicker(t *testing.T) {
	raw := json.RawMessage(`{
		"time_ms": 1615366379123,
		"channel": "futures.book_ticker",
		"event": "update",
		"result": {
			"t": 1615366379123,
			"u": 2517661076,
			"s": "ETH_USDT",
			"b": "54696.6",
			"B": "37000",
			"a": "54696.7",
			"A": 47061
		}
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "usdt", "1m")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handler.bookTicker.UpdateID != 2517661076 {
		t.Fatalf("update id = %d, want 2517661076", handler.bookTicker.UpdateID)
	}
	if handler.bookTicker.AskQuantity != "47061" {
		t.Fatalf("ask quantity = %q, want 47061", handler.bookTicker.AskQuantity)
	}
}

func TestDispatchContractStats(t *testing.T) {
	raw := json.RawMessage(`{
		"time_ms": 1541659086123,
		"channel": "futures.contract_stats",
		"event": "update",
		"result": [{
			"time": 1603865400,
			"contract": "ETH_USDT",
			"mark_price": "8865",
			"open_interest": 124724
		}]
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "usdt", "1m")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handler.markPrice.MarkPrice != "8865" {
		t.Fatalf("mark price = %q, want 8865", handler.markPrice.MarkPrice)
	}
	if handler.openInterest.OpenInterest != "124724" {
		t.Fatalf("open interest = %q, want 124724", handler.openInterest.OpenInterest)
	}
}

func TestDispatchLiquidation(t *testing.T) {
	raw := json.RawMessage(`{
		"time_ms": 1541505434123,
		"channel": "futures.public_liquidates",
		"event": "update",
		"result": [{
			"price": 215.1,
			"size": "-124",
			"time_ms": 1541486601123,
			"contract": "ETH_USDT"
		}]
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "usdt", "1m")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handler.liquidation.Symbol != "ETH_USDT" {
		t.Fatalf("symbol = %q, want ETH_USDT", handler.liquidation.Symbol)
	}
	if handler.liquidation.Price != "215.1" {
		t.Fatalf("price = %q, want 215.1", handler.liquidation.Price)
	}
	if handler.liquidation.Side != "" {
		t.Fatalf("side = %q, want empty", handler.liquidation.Side)
	}
}

func TestDispatchSingleObjectLiquidation(t *testing.T) {
	raw := json.RawMessage(`{
		"time_ms": 1541505434123,
		"channel": "futures.public_liquidates",
		"event": "update",
		"result": {
			"price": 215.1,
			"size": "-124",
			"time_ms": 1541486601123,
			"contract": "ETH_USDT"
		}
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "usdt", "1m")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handler.liquidation.Symbol != "ETH_USDT" {
		t.Fatalf("symbol = %q, want ETH_USDT", handler.liquidation.Symbol)
	}
	if handler.liquidation.Price != "215.1" {
		t.Fatalf("price = %q, want 215.1", handler.liquidation.Price)
	}
}

func TestParseStreamName(t *testing.T) {
	interval, symbol, err := parseStreamName("1h_ETH_USDT")
	if err != nil {
		t.Fatalf("parseStreamName: %v", err)
	}
	if interval != "1h" || symbol != "ETH_USDT" {
		t.Fatalf("parsed = %q %q, want 1h ETH_USDT", interval, symbol)
	}
}

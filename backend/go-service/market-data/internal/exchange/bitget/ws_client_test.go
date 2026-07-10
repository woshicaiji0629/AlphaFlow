package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"alphaflow/go-service/market-data/internal/model"
)

type fakeHandler struct {
	lastPrice    model.LastPrice
	markPrice    model.MarkPrice
	bookTicker   model.BookTicker
	openInterest model.OpenInterest
	kline        model.Kline
	klines       []model.Kline
}

func (h *fakeHandler) HandleKline(_ context.Context, kline model.Kline) error {
	h.kline = kline
	h.klines = append(h.klines, kline)
	return nil
}

func TestDispatchKlineKeepsCurrentCandleOpen(t *testing.T) {
	openTime := int64(1700000000000)
	raw := klineMessage("update", openTime+30000, openTime)
	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "USDT-FUTURES")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(handler.klines) != 1 || handler.klines[0].IsClosed {
		t.Fatalf("klines = %#v, want one open candle", handler.klines)
	}
	if handler.klines[0].EventTime != openTime+30000 {
		t.Fatalf("event time = %d", handler.klines[0].EventTime)
	}
}

func TestDispatchKlineClosesPreviousCandleOnRollover(t *testing.T) {
	openTime := int64(1700000000000)
	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "USDT-FUTURES")
	if err := client.dispatch(context.Background(), klineMessage("update", openTime+30000, openTime), handler); err != nil {
		t.Fatalf("dispatch first candle: %v", err)
	}
	if err := client.dispatch(context.Background(), klineMessage("update", openTime+61000, openTime+60000), handler); err != nil {
		t.Fatalf("dispatch next candle: %v", err)
	}
	if len(handler.klines) != 3 {
		t.Fatalf("klines = %#v, want open, closed, open", handler.klines)
	}
	if !handler.klines[1].IsClosed || handler.klines[1].OpenTime != openTime {
		t.Fatalf("closed candle = %#v", handler.klines[1])
	}
	if handler.klines[2].IsClosed || handler.klines[2].OpenTime != openTime+60000 {
		t.Fatalf("next candle = %#v", handler.klines[2])
	}
}

func TestDispatchKlineClosesCurrentCandleOnlyOnce(t *testing.T) {
	openTime := int64(1700000000000)
	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "USDT-FUTURES")
	if err := client.dispatch(context.Background(), klineMessage("update", openTime+30000, openTime), handler); err != nil {
		t.Fatalf("dispatch open candle: %v", err)
	}
	if err := client.dispatch(context.Background(), klineMessage("update", openTime+60000, openTime), handler); err != nil {
		t.Fatalf("dispatch closed candle: %v", err)
	}
	if err := client.dispatch(context.Background(), klineMessage("update", openTime+61000, openTime), handler); err != nil {
		t.Fatalf("dispatch duplicate closed candle: %v", err)
	}
	if len(handler.klines) != 2 || !handler.klines[1].IsClosed {
		t.Fatalf("klines = %#v, want one open and one closed event", handler.klines)
	}
}

func TestDispatchKlineSkipsClosedSnapshotWithoutOpenState(t *testing.T) {
	openTime := int64(1700000000000)
	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "USDT-FUTURES")
	if err := client.dispatch(context.Background(), klineMessage("snapshot", openTime+120000, openTime), handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(handler.klines) != 0 {
		t.Fatalf("klines = %#v, want closed snapshot handled by REST backfill", handler.klines)
	}
}

func klineMessage(action string, eventTime int64, openTime int64) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
		"action": %q,
		"arg": {"instType":"USDT-FUTURES","channel":"candle1m","instId":"ETHUSDT"},
		"ts": %d,
		"data": [[%q,"1","2","0.5","1.5","10","15","15"]]
	}`, action, eventTime, strconv.FormatInt(openTime, 10)))
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

func (h *fakeHandler) HandleLiquidation(context.Context, model.Liquidation) error {
	return nil
}

func TestDispatchTickerAcceptsNumericTimestamps(t *testing.T) {
	raw := json.RawMessage(`{
		"action": "snapshot",
		"arg": {
			"instType": "USDT-FUTURES",
			"channel": "ticker",
			"instId": "ETHUSDT"
		},
		"ts": 1700000000000,
		"data": [{
			"instId": "ETHUSDT",
			"lastPr": 3000.5,
			"markPrice": "3000.4",
			"indexPrice": 3000.3,
			"fundingRate": "0.0001",
			"nextFundingTime": 1700003600000,
			"bidPr": "3000.1",
			"bidSz": 12,
			"askPr": 3000.2,
			"askSz": "8",
			"holdingAmount": 12345,
			"ts": 1700000000123
		}]
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "USDT-FUTURES")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handler.lastPrice.Price != "3000.5" {
		t.Fatalf("last price = %q, want 3000.5", handler.lastPrice.Price)
	}
	if handler.markPrice.NextFundingTime != 1700003600000 {
		t.Fatalf("next funding time = %d, want 1700003600000", handler.markPrice.NextFundingTime)
	}
	if handler.bookTicker.BidQuantity != "12" || handler.bookTicker.AskPrice != "3000.2" {
		t.Fatalf("unexpected book ticker: %#v", handler.bookTicker)
	}
	if handler.openInterest.OpenInterest != "12345" {
		t.Fatalf("open interest = %q, want 12345", handler.openInterest.OpenInterest)
	}
}

func TestDispatchTradeAcceptsStringTimestamp(t *testing.T) {
	raw := json.RawMessage(`{
		"action": "snapshot",
		"arg": {
			"instType": "USDT-FUTURES",
			"channel": "trade",
			"instId": "ETHUSDT"
		},
		"ts": "1700000000000",
		"data": [{
			"instId": "ETHUSDT",
			"tradeId": "123456",
			"price": "3000.5",
			"size": "0.2",
			"ts": "1700000000123"
		}]
	}`)

	handler := &fakeHandler{}
	client := NewWSClient("wss://example.test", "USDT-FUTURES")
	if err := client.dispatch(context.Background(), raw, handler); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handler.lastPrice.TradeID != 123456 {
		t.Fatalf("trade id = %d, want 123456", handler.lastPrice.TradeID)
	}
	if handler.lastPrice.EventTime != 1700000000123 {
		t.Fatalf("event time = %d, want 1700000000123", handler.lastPrice.EventTime)
	}
}

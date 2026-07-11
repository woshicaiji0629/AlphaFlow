package clob

import (
	"context"
	"testing"
	"time"

	"alphaflow/go-service/polymarket-research/internal/model"
)

type testSink struct {
	books       []model.BookTick
	trades      []model.Trade
	resolutions []model.Resolution
}

func (s *testSink) WriteBookTick(_ context.Context, v model.BookTick) error {
	s.books = append(s.books, v)
	return nil
}
func (s *testSink) WriteTrade(_ context.Context, v model.Trade) error {
	s.trades = append(s.trades, v)
	return nil
}
func (s *testSink) WriteResolution(_ context.Context, v model.Resolution) error {
	s.resolutions = append(s.resolutions, v)
	return nil
}

func TestHandleMarketMessages(t *testing.T) {
	sink := &testSink{}
	client := New("ws://example", sink, time.Second)
	client.now = func() time.Time { return time.UnixMilli(2000) }
	client.UpdateMarkets([]model.Market{{MarketID: "m1", YesTokenID: "yes", NoTokenID: "no", AcceptingOrders: true, StartTimeMS: 1000, EndTimeMS: 3000}})
	if err := client.handle(context.Background(), []byte(`[{"event_type":"best_bid_ask","asset_id":"yes","best_bid":"0.45","best_ask":"0.48","spread":"0.03","timestamp":"1000"},{"event_type":"last_trade_price","asset_id":"no","price":"0.52","side":"BUY","size":"10","fee_rate_bps":"20","timestamp":"1001"}]`)); err != nil {
		t.Fatal(err)
	}
	if len(sink.books) != 1 || sink.books[0].Outcome != "up" || sink.books[0].ReceivedAtMS != 2000 {
		t.Fatalf("books=%+v", sink.books)
	}
	if len(sink.trades) != 1 || sink.trades[0].Outcome != "down" || sink.trades[0].FeeRateBPS != "20" {
		t.Fatalf("trades=%+v", sink.trades)
	}
	if err := client.handle(context.Background(), []byte(`{"event_type":"market_resolved","market":"condition-id","winning_asset_id":"yes","winning_outcome":"Yes","timestamp":"1002"}`)); err != nil {
		t.Fatal(err)
	}
	if len(sink.resolutions) != 1 || sink.resolutions[0].MarketID != "m1" || sink.resolutions[0].WinningOutcome != "up" {
		t.Fatalf("resolutions=%+v", sink.resolutions)
	}
}

func TestUpdateMarketsSignalsOnlyForChanges(t *testing.T) {
	client := New("ws://example", &testSink{}, time.Second)
	client.now = func() time.Time { return time.UnixMilli(2000) }
	markets := []model.Market{{MarketID: "m1", YesTokenID: "yes", NoTokenID: "no", AcceptingOrders: true, StartTimeMS: 1000, EndTimeMS: 3000}}
	client.UpdateMarkets(markets)
	<-client.changed
	client.UpdateMarkets(markets)
	select {
	case <-client.changed:
		t.Fatal("unchanged markets triggered a reconnect")
	default:
	}
}

func TestHandleRejectsInvalidTimestamp(t *testing.T) {
	client := New("ws://example", &testSink{}, time.Second)
	if err := client.handle(context.Background(), []byte(`{"event_type":"best_bid_ask","timestamp":"invalid"}`)); err == nil {
		t.Fatal("expected timestamp error")
	}
}

func TestUpdateMarketsExcludesClosedMarkets(t *testing.T) {
	client := New("ws://example", &testSink{}, time.Second)
	client.UpdateMarkets([]model.Market{{MarketID: "closed", YesTokenID: "a", NoTokenID: "b", Closed: true}})
	client.mu.RLock()
	count := len(client.tokens)
	client.mu.RUnlock()
	if count != 0 {
		t.Fatalf("tokens=%d", count)
	}
}

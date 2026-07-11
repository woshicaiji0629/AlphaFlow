package rtds

import (
	"context"
	"testing"
	"time"

	"alphaflow/go-service/polymarket-research/internal/model"
)

type testSink struct{ prices []model.ReferencePrice }

func (s *testSink) WriteReferencePrice(_ context.Context, v model.ReferencePrice) error {
	s.prices = append(s.prices, v)
	return nil
}

func TestHandleReferencePrices(t *testing.T) {
	sink := &testSink{}
	client := New("ws://example", sink, time.Second)
	client.now = func() time.Time { return time.UnixMilli(2000) }
	if err := client.handle(context.Background(), []byte(`{"topic":"crypto_prices_chainlink","type":"update","payload":{"symbol":"sol/usd","timestamp":1000,"value":123.45}}`)); err != nil {
		t.Fatal(err)
	}
	if len(sink.prices) != 1 || sink.prices[0].Source != "chainlink" || sink.prices[0].Symbol != "SOL" || sink.prices[0].Price != "123.45" {
		t.Fatalf("prices=%+v", sink.prices)
	}
}

func TestHandleIgnoresUnverifiedReferenceSymbol(t *testing.T) {
	sink := &testSink{}
	client := New("ws://example", sink, time.Second)
	if err := client.handle(context.Background(), []byte(`{"topic":"crypto_prices","type":"update","payload":{"symbol":"dogeusdt","timestamp":1000,"value":0.2}}`)); err != nil {
		t.Fatal(err)
	}
	if len(sink.prices) != 0 {
		t.Fatalf("prices=%+v", sink.prices)
	}
}

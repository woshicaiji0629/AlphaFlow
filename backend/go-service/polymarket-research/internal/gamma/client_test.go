package gamma

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type fakeHTTP struct{ pages map[string]string }

func (f fakeHTTP) Get(_ context.Context, _ string, query map[string]string) ([]byte, error) {
	key := query["closed"] + ":" + query["offset"]
	body, ok := f.pages[key]
	if !ok {
		return nil, fmt.Errorf("unexpected page %s", key)
	}
	return []byte(body), nil
}

func TestDiscoverFiltersAndMapsFifteenMinuteCryptoMarkets(t *testing.T) {
	payload := `[{"id":"event-1","markets":[
		{"id":"1","question":"Bitcoin Up or Down","conditionId":"condition-1","slug":"btc-updown-15m-1780000000","resolutionSource":"chainlink","startDate":"2026-05-28T00:00:00Z","endDate":"2026-05-28T00:15:00Z","outcomes":"[\"Up\",\"Down\"]","outcomePrices":"[\"0.5\",\"0.5\"]","clobTokenIds":"[\"up-token\",\"down-token\"]","active":true,"closed":false,"enableOrderBook":true,"acceptingOrders":true,"updatedAt":"2026-05-28T00:01:00Z","events":[{"id":"event-1"}]},
		{"id":"2","question":"Dogecoin Up or Down","conditionId":"condition-2","slug":"doge-updown-15m-1780000000","startDate":"2026-05-28T00:00:00Z","endDate":"2026-05-28T00:15:00Z","outcomes":"[\"Up\",\"Down\"]","clobTokenIds":"[\"a\",\"b\"]","active":true,"enableOrderBook":true},
		{"id":"3","question":"ETH hourly","conditionId":"condition-3","slug":"eth-updown-1h-1780000000","startDate":"2026-05-28T00:00:00Z","endDate":"2026-05-28T01:00:00Z","outcomes":"[\"Up\",\"Down\"]","clobTokenIds":"[\"a\",\"b\"]","active":true,"enableOrderBook":true}
	]}]`
	client := New(Options{BaseURL: "https://example.test", PageSize: 100, HTTPClient: fakeHTTP{pages: map[string]string{"false:0": payload, "true:0": "[]"}}, Now: func() time.Time { return time.Unix(0, 0) }})
	markets, err := client.Discover(context.Background(), []string{"BTC", "ETH", "SOL", "XRP"}, []string{"15m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(markets) != 1 {
		t.Fatalf("markets=%d, want 1", len(markets))
	}
	got := markets[0]
	if got.Symbol != "BTC" || got.YesTokenID != "up-token" || got.NoTokenID != "down-token" || got.EventID != "event-1" {
		t.Fatalf("unexpected market: %+v", got)
	}
}

func TestDiscoverRejectsMalformedSelectedMarket(t *testing.T) {
	payload := `[{"id":"event","markets":[{"id":"1","slug":"btc-updown-15m-1780000000","startDate":"bad","endDate":"2026-05-28T00:15:00Z","active":true,"enableOrderBook":true}]}]`
	client := New(Options{BaseURL: "https://example.test", PageSize: 100, HTTPClient: fakeHTTP{pages: map[string]string{"false:0": payload, "true:0": "[]"}}})
	if _, err := client.Discover(context.Background(), []string{"BTC"}, []string{"15m"}); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestDiscoverIncludesRecentlyClosedMarketForResolutionReconciliation(t *testing.T) {
	payload := `[{"id":"event","markets":[{"id":"closed-1","question":"XRP Up or Down","conditionId":"condition","slug":"xrp-updown-15m-1780000000","startDate":"2026-05-28T00:00:00Z","endDate":"2026-05-28T00:15:00Z","outcomes":"[\"Up\",\"Down\"]","outcomePrices":"[\"0\",\"1\"]","clobTokenIds":"[\"up\",\"down\"]","active":false,"closed":true,"enableOrderBook":false}]}]`
	client := New(Options{BaseURL: "https://example.test", PageSize: 100, HTTPClient: fakeHTTP{pages: map[string]string{"false:0": "[]", "true:0": payload}}})
	markets, err := client.Discover(context.Background(), []string{"XRP"}, []string{"15m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(markets) != 1 || !markets[0].Closed || markets[0].ResolvedOutcome != "down" {
		t.Fatalf("unexpected closed markets: %+v", markets)
	}
}

func TestDiscoverMapsFiveMinuteMarkets(t *testing.T) {
	payload := `[{"id":"event","markets":[
		{"id":"btc-5m","question":"Bitcoin Up or Down","conditionId":"btc-condition","slug":"btc-updown-5m-1780000000","startDate":"2026-05-28T00:00:00Z","endDate":"2026-05-28T00:05:00Z","outcomes":"[\"Up\",\"Down\"]","clobTokenIds":"[\"btc-up\",\"btc-down\"]","active":true,"enableOrderBook":true},
		{"id":"sol-wrong-window","question":"Solana Up or Down","conditionId":"sol-condition","slug":"sol-updown-5m-1780000000","startDate":"2026-05-28T00:00:00Z","endDate":"2026-05-28T00:15:00Z","outcomes":"[\"Up\",\"Down\"]","clobTokenIds":"[\"sol-up\",\"sol-down\"]","active":true,"enableOrderBook":true}
	]}]`
	client := New(Options{BaseURL: "https://example.test", PageSize: 100, HTTPClient: fakeHTTP{pages: map[string]string{"false:0": payload, "true:0": "[]"}}})
	markets, err := client.Discover(context.Background(), []string{"BTC", "SOL"}, []string{"5m", "15m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(markets) != 1 || markets[0].Duration != "5m" || markets[0].MarketID != "btc-5m" {
		t.Fatalf("unexpected five minute markets: %+v", markets)
	}
}

func TestSymbolFromSlugSupportsExpandedCryptoSet(t *testing.T) {
	tests := map[string]string{
		"doge-updown-5m-1":        "DOGE",
		"dogecoin-updown-15m-1":   "DOGE",
		"bnb-updown-5m-1":         "BNB",
		"hype-updown-15m-1":       "HYPE",
		"hyperliquid-updown-5m-1": "HYPE",
		"ripple-updown-15m-1":     "XRP",
	}
	for slug, want := range tests {
		got, ok := symbolFromSlug(slug)
		if !ok || got != want {
			t.Errorf("symbolFromSlug(%q)=(%q,%t), want (%q,true)", slug, got, ok, want)
		}
	}
	if _, ok := symbolFromSlug("ada-updown-5m-1"); ok {
		t.Fatal("unsupported ADA slug should be rejected")
	}
}

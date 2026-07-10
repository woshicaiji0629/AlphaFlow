package weex

import (
	"context"
	"net/http"
	"testing"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
)

func TestDemoAccountAndOrderEndpoints(t *testing.T) {
	c := &captureClient{body: []byte(`[{"asset":"SUSDT","balance":"100","availableBalance":"80","unrealizePnl":"2"}]`)}
	a, _ := New(Options{Account: account(), Credential: credential(), BaseURL: "https://example.test", HTTPClient: c})
	got, err := a.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.request.URL.Path != "/capi/v3/sim/balance" || c.request.Header.Get("ACCESS-SIGN") == "" || got.AvailableBalance != "80" {
		t.Fatalf("request=%v account=%#v", c.request, got)
	}
	c.body = []byte(`{"orderId":"o1"}`)
	report, err := a.Execute(context.Background(), execution.OrderIntent{IntentID: "i1", Symbol: "BTCUSDT", Side: execution.OrderSideBuy, PositionSide: "long", Type: execution.OrderTypeMarket, Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if c.request.URL.Path != "/capi/v3/sim/order" || report.ExchangeOrderID != "o1" {
		t.Fatalf("request=%v report=%#v", c.request, report)
	}
}
func TestLiveOrderUsesV2Direction(t *testing.T) {
	c := &captureClient{body: []byte(`{"order_id":"o1"}`)}
	a, _ := New(Options{Account: liveAccount(), Credential: credential(), BaseURL: "https://example.test", HTTPClient: c})
	_, err := a.Execute(context.Background(), execution.OrderIntent{IntentID: "i", Symbol: "cmt_btcusdt", Side: execution.OrderSideSell, PositionSide: "short", Type: execution.OrderTypeMarket, Quantity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if c.request.Method != http.MethodPost || c.request.URL.Path != "/capi/v2/order/placeOrder" {
		t.Fatalf("request=%v", c.request)
	}
}
func TestParsePrivateOrderEvent(t *testing.T) {
	events, err := parsePrivateEvents(liveAccount(), map[string]any{"e": "orders", "E": float64(200), "v": float64(3), "d": []any{map[string]any{"id": "o1", "symbol": "BTCUSDT", "clientOrderId": "af-i", "orderSide": "BUY", "positionSide": "LONG", "type": "MARKET", "status": "FILLED", "size": "2", "cumFillSize": "2", "latestFillPrice": "100", "updatedTime": "200"}}})
	if err != nil || len(events) != 1 || events[0].Order.Status != execution.ExecutionStatusFilled || events[0].Report.FilledQuantity != 2 {
		t.Fatalf("events=%#v err=%v", events, err)
	}
}
func account() executionaccount.Account {
	return executionaccount.Account{ID: "a", Exchange: "weex", Environment: executionaccount.EnvironmentTestnet, Enabled: true, TradingEnabled: true}
}
func liveAccount() executionaccount.Account {
	return executionaccount.Account{ID: "a", Exchange: "weex", Environment: executionaccount.EnvironmentLive, Enabled: true, TradingEnabled: true, LiveConfirmed: true}
}
func credential() executionaccount.Credential {
	return executionaccount.Credential{APIKey: "key", APISecret: "secret", Passphrase: "pass"}
}

type captureClient struct {
	request *http.Request
	body    []byte
}

func (c *captureClient) DoBytes(r *http.Request) ([]byte, error) { c.request = r; return c.body, nil }

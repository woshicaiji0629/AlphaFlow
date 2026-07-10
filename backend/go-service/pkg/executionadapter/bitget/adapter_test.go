package bitget

import (
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
	"context"
	"net/http"
	"testing"
	"time"
)

func TestExecuteDemoMarketOrder(t *testing.T) {
	client := &captureClient{body: []byte(`{"code":"00000","requestTime":200,"data":{"orderId":"o1"}}`)}
	a, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "bitget", Environment: executionaccount.EnvironmentTestnet, Enabled: true, TradingEnabled: true, PositionMode: executionaccount.PositionModeOneWay, MarginMode: executionaccount.MarginModeCross}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s", Passphrase: "p"}, BaseURL: "https://example.test", HTTPClient: client})
	report, err := a.Execute(context.Background(), execution.OrderIntent{IntentID: "intent:1", Symbol: "ETHUSDT", Side: execution.OrderSideBuy, Quantity: 1, ReduceOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if client.request.Method != http.MethodPost || client.request.Header.Get("paptrading") != "1" || report.ExchangeOrderID != "o1" {
		t.Fatalf("request=%v report=%#v", client.request, report)
	}
}

func TestDemoAccountAddsSignedHeaders(t *testing.T) {
	client := &captureClient{body: []byte(`{"code":"00000","requestTime":123,"data":[{"marginCoin":"USDT","accountEquity":"100","available":"80","unrealizedPL":"2"}]}`)}
	a, err := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "bitget", Environment: executionaccount.EnvironmentTestnet, Market: "um"}, Credential: executionaccount.Credential{APIKey: "key", APISecret: "secret", Passphrase: "pass"}, BaseURL: "https://example.test", HTTPClient: client, Now: func() time.Time { return time.UnixMilli(1000) }})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client.request.Header.Get("paptrading") != "1" || client.request.Header.Get("ACCESS-SIGN") == "" || client.request.Header.Get("ACCESS-PASSPHRASE") != "pass" {
		t.Fatalf("headers=%v", client.request.Header)
	}
	if got.AvailableBalance != "80" {
		t.Fatalf("account=%#v", got)
	}
}
func TestLiveAccountOmitsDemoHeader(t *testing.T) {
	client := &captureClient{body: []byte(`{"code":"00000","data":[{"marginCoin":"USDT"}]}`)}
	a, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "bitget", Environment: executionaccount.EnvironmentLive}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s", Passphrase: "p"}, BaseURL: "https://example.test", HTTPClient: client})
	_, _ = a.Account(context.Background())
	if client.request.Header.Get("paptrading") != "" {
		t.Fatal("live request contains demo header")
	}
}

func TestOpenOrdersMapsPendingOrders(t *testing.T) {
	client := &captureClient{body: []byte(`{"code":"00000","data":{"entrustedList":[{"symbol":"ETHUSDT","size":"2","orderId":"o1","clientOid":"i1","baseVolume":"0.5","priceAvg":"1903","status":"partially_filled","side":"buy","posSide":"long","orderType":"limit","reduceOnly":"YES","cTime":"100","uTime":"200"}]}}`)}
	a, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "bitget", Environment: executionaccount.EnvironmentTestnet}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s", Passphrase: "p"}, BaseURL: "https://example.test", HTTPClient: client})
	orders, err := a.OpenOrders(context.Background(), "ETHUSDT")
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].ClientOrderID != "i1" || orders[0].FilledQuantity != 0.5 || !orders[0].ReduceOnly {
		t.Fatalf("orders=%#v", orders)
	}
	if client.request.URL.Query().Get("symbol") != "ETHUSDT" || client.request.Header.Get("paptrading") != "1" {
		t.Fatalf("request=%s headers=%v", client.request.URL, client.request.Header)
	}
}

func TestCapabilityMapsContractConfig(t *testing.T) {
	client := &captureClient{body: []byte(`{"code":"00000","requestTime":123,"data":[{"symbol":"ETHUSDT","minTradeNum":"0.01","priceEndStep":"5","pricePlace":"2","sizeMultiplier":"0.01","minTradeUSDT":"5","maxLever":"125","maxMarketOrderQty":"220"}]}`)}
	a, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "bitget", Environment: executionaccount.EnvironmentTestnet, Market: "um"}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s", Passphrase: "p"}, BaseURL: "https://example.test", HTTPClient: client})
	got, err := a.Capability(context.Background(), "ETHUSDT")
	if err != nil {
		t.Fatal(err)
	}
	if got.QtyStep != "0.01" || got.PriceTick != "0.05" || got.MinNotional != "5" || got.MaxLeverage != "125" || got.MaxOrderQty != "220" {
		t.Fatalf("capability=%#v", got)
	}
}

type captureClient struct {
	request *http.Request
	body    []byte
}

func (c *captureClient) DoBytes(r *http.Request) ([]byte, error) { c.request = r; return c.body, nil }

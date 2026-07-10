package gate

import (
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestExecuteOneWayMarketOrder(t *testing.T) {
	client := &captureClient{body: []byte(`{"id":12,"status":"finished","finish_as":"filled","size":"-10","left":"0","fill_price":"1900","finish_time":200}`)}
	a, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "gate", Environment: executionaccount.EnvironmentTestnet, Enabled: true, TradingEnabled: true, PositionMode: executionaccount.PositionModeOneWay}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s"}, BaseURL: "https://example.test/api/v4", HTTPClient: client})
	report, err := a.Execute(context.Background(), execution.OrderIntent{IntentID: "intent:1", Symbol: "ETH_USDT", Side: execution.OrderSideSell, Quantity: 10, ReduceOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if client.request.Method != http.MethodPost || report.Status != execution.ExecutionStatusFilled || report.FilledQuantity != 10 {
		t.Fatalf("request=%v report=%#v", client.request, report)
	}
}

func TestAccountSignsPrivateRequest(t *testing.T) {
	client := &captureClient{body: []byte(`{"total":"100","available":"80","unrealised_pnl":"2"}`)}
	a, err := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "gate", Environment: executionaccount.EnvironmentTestnet, Market: "um"}, Credential: executionaccount.Credential{APIKey: "key", APISecret: "secret"}, BaseURL: "https://example.test/api/v4", HTTPClient: client, Now: func() time.Time { return time.Unix(1000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client.request.Header.Get("KEY") != "key" || client.request.Header.Get("SIGN") == "" || client.request.Header.Get("Timestamp") != "1000" {
		t.Fatalf("headers=%v", client.request.Header)
	}
	if strings.Contains(client.request.URL.String(), "secret") {
		t.Fatal("secret leaked")
	}
	if got.AvailableBalance != "80" || got.UnrealizedPnL != "2" || got.Scope != "testnet" {
		t.Fatalf("account=%#v", got)
	}
}
func TestNewSelectsEnvironmentURL(t *testing.T) {
	testnet, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "gate", Environment: executionaccount.EnvironmentTestnet}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s"}})
	live, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "gate", Environment: executionaccount.EnvironmentLive}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s"}})
	if testnet.baseURL != testnetURL || live.baseURL != liveURL {
		t.Fatalf("urls=%s,%s", testnet.baseURL, live.baseURL)
	}
}

func TestOpenOrdersMapsContractQuantity(t *testing.T) {
	client := &captureClient{body: []byte(`[{"id":12,"text":"t-intent","contract":"ETH_USDT","size":"-10","left":"-4","price":"0","fill_price":"1900","status":"open","tif":"ioc","is_reduce_only":true,"create_time":100}]`)}
	a, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "gate", Environment: executionaccount.EnvironmentTestnet}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s"}, BaseURL: "https://example.test/api/v4", HTTPClient: client})
	orders, err := a.OpenOrders(context.Background(), "ETH_USDT")
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].Side != "sell" || orders[0].Quantity != 10 || orders[0].FilledQuantity != 6 || orders[0].Type != "market" || !orders[0].ReduceOnly {
		t.Fatalf("orders=%#v", orders)
	}
}
func TestCapabilityUsesContractMultiplier(t *testing.T) {
	client := &captureClient{body: []byte(`{"name":"ETH_USDT","quanto_multiplier":"0.0001","order_price_round":"0.01","order_size_min":"1","order_size_max":"1000","leverage_max":"100","market_order_size_max":"0","config_change_time":123}`)}
	a, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "gate", Environment: executionaccount.EnvironmentTestnet, Market: "um"}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s"}, BaseURL: "https://example.test/api/v4", HTTPClient: client})
	got, err := a.Capability(context.Background(), "ETH_USDT")
	if err != nil {
		t.Fatal(err)
	}
	if got.ContractSize != "0.0001" || got.PriceTick != "0.01" || got.MaxOrderQty != "1000" || got.MaxLeverage != "100" {
		t.Fatalf("capability=%#v", got)
	}
}

type captureClient struct {
	request *http.Request
	body    []byte
}

func (c *captureClient) DoBytes(r *http.Request) ([]byte, error) { c.request = r; return c.body, nil }

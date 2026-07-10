package hotcoin

import (
	"context"
	"net/http"
	"testing"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
)

func TestAccountSignsAndMapsAssets(t *testing.T) {
	c := &captureClient{body: []byte(`[{"currencyCode":"USDT","availableBalance":"80","positionAccountRights":"100","unRealizedSurplus":"2"}]`)}
	a, _ := New(Options{Account: account(), Credential: credential(), BaseURL: "https://example.test", HTTPClient: c})
	got, err := a.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.request.URL.Query().Get("Signature") == "" || got.AvailableBalance != "80" {
		t.Fatalf("request=%v account=%#v", c.request, got)
	}
}
func TestExecuteMapsMarketSide(t *testing.T) {
	c := &captureClient{body: []byte(`{"id":123}`)}
	a, _ := New(Options{Account: tradingAccount(), Credential: credential(), BaseURL: "https://example.test", HTTPClient: c})
	report, err := a.Execute(context.Background(), execution.OrderIntent{IntentID: "i", Symbol: "btcusdt", Side: execution.OrderSideSell, PositionSide: "short", Type: execution.OrderTypeMarket, Quantity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if c.request.Method != http.MethodPost || report.ExchangeOrderID != "123" {
		t.Fatalf("request=%v report=%#v", c.request, report)
	}
}
func TestOpenOrdersRequiresSymbol(t *testing.T) {
	a, _ := New(Options{Account: account(), Credential: credential()})
	if _, err := a.OpenOrders(context.Background(), ""); err == nil {
		t.Fatal("OpenOrders() error=nil")
	}
}
func TestCapabilityMapsPublicContract(t *testing.T) {
	c := &captureClient{body: []byte(`{"code":200,"data":[{"code":"btcusdt","minTradeDigit":3,"minQuoteDigit":2,"maxLever":100,"unitAmount":1}]}`)}
	a, _ := New(Options{Account: account(), Credential: credential(), BaseURL: "https://example.test", HTTPClient: c})
	got, err := a.Capability(context.Background(), "btcusdt")
	if err != nil {
		t.Fatal(err)
	}
	if c.request.URL.Path != "/api/v1/perpetual/public" || got.QtyStep != "0.001" || got.PriceTick != "0.01" || got.ContractSize != "1" {
		t.Fatalf("request=%v capability=%#v", c.request, got)
	}
}
func TestRejectsUnsupportedTestnet(t *testing.T) {
	a := account()
	a.Environment = executionaccount.EnvironmentTestnet
	if _, err := New(Options{Account: a, Credential: credential()}); err == nil {
		t.Fatal("New() error=nil")
	}
}
func account() executionaccount.Account {
	return executionaccount.Account{ID: "a", Exchange: "hotcoin", Environment: executionaccount.EnvironmentLive, Market: "um"}
}
func tradingAccount() executionaccount.Account {
	a := account()
	a.Enabled = true
	a.TradingEnabled = true
	a.LiveConfirmed = true
	return a
}
func credential() executionaccount.Credential {
	return executionaccount.Credential{APIKey: "key", APISecret: "secret"}
}

type captureClient struct {
	request *http.Request
	body    []byte
}

func (c *captureClient) DoBytes(r *http.Request) ([]byte, error) { c.request = r; return c.body, nil }

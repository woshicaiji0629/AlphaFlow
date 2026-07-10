package deepcoin

import (
	"context"
	"net/http"
	"testing"
	"time"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
	"alphaflow/go-service/pkg/strategy"
)

func TestAccountSignsAndMapsBalance(t *testing.T) {
	c := &captureClient{body: []byte(`{"code":"0","data":[{"ccy":"USDT","eq":"100","availBal":"80","upl":"2"}]}`)}
	a, err := New(Options{Account: account(), Credential: credential(), BaseURL: "https://example.test", HTTPClient: c, Now: func() time.Time { return time.UnixMilli(1000) }})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.request.Header.Get("DC-ACCESS-SIGN") == "" || c.request.Header.Get("DC-ACCESS-PASSPHRASE") != "pass" || got.AvailableBalance != "80" {
		t.Fatalf("request=%v account=%#v", c.request, got)
	}
}
func TestExecuteUsesClientOrderID(t *testing.T) {
	c := &captureClient{body: []byte(`{"code":"0","data":{"ordId":"o1","sCode":"0"}}`)}
	a, _ := New(Options{Account: tradingAccount(), Credential: credential(), BaseURL: "https://example.test", HTTPClient: c})
	report, err := a.Execute(context.Background(), execution.OrderIntent{IntentID: "intent:1", Symbol: "BTC-USDT-SWAP", Side: execution.OrderSideBuy, PositionSide: "long", Type: execution.OrderTypeMarket, Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.ExchangeOrderID != "o1" || c.request.Method != http.MethodPost || c.request.URL.Path != "/deepcoin/trade/order" {
		t.Fatalf("request=%v report=%#v", c.request, report)
	}
}
func TestRejectsUnsupportedTestnet(t *testing.T) {
	a := account()
	a.Environment = executionaccount.EnvironmentTestnet
	if _, err := New(Options{Account: a, Credential: credential()}); err == nil {
		t.Fatal("New() error=nil")
	}
}
func TestParsePrivatePositionAndTrade(t *testing.T) {
	events, err := parsePrivateEvents(account(), map[string]any{"result": []any{map[string]any{"table": "Position", "data": map[string]any{"I": "BTCUSDT", "p": "1", "Po": float64(2), "OP": float64(100), "U": float64(3)}}, map[string]any{"table": "Trade", "data": map[string]any{"OS": "o1", "V": float64(2), "P": float64(101), "F": float64(0.1), "TT": float64(4)}}}})
	if err != nil || len(events) != 2 || events[0].Position.Side != strategy.PositionSideShort || events[1].Report.Fee != 0.1 {
		t.Fatalf("events=%#v err=%v", events, err)
	}
}
func account() executionaccount.Account {
	return executionaccount.Account{ID: "a", Exchange: "deepcoin", Environment: executionaccount.EnvironmentLive, Market: "um"}
}
func tradingAccount() executionaccount.Account {
	a := account()
	a.Enabled = true
	a.TradingEnabled = true
	a.LiveConfirmed = true
	return a
}
func credential() executionaccount.Credential {
	return executionaccount.Credential{APIKey: "key", APISecret: "secret", Passphrase: "pass"}
}

type captureClient struct {
	request *http.Request
	body    []byte
}

func (c *captureClient) DoBytes(r *http.Request) ([]byte, error) { c.request = r; return c.body, nil }

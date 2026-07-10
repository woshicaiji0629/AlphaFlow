package binance

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"alphaflow/go-service/pkg/executionaccount"
)

func TestAccountSignsPrivateRequest(t *testing.T) {
	client := &captureClient{body: []byte(`[{"asset":"USDT","balance":"100","availableBalance":"80","crossUnPnl":"2","updateTime":123}]`)}
	adapter, err := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "binance", Environment: executionaccount.EnvironmentTestnet, Market: "um"}, Credential: executionaccount.Credential{APIKey: "key", APISecret: "secret"}, BaseURL: "https://example.test", HTTPClient: client, Now: func() time.Time { return time.UnixMilli(1000) }})
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client.request.Header.Get("X-MBX-APIKEY") != "key" {
		t.Fatalf("api key=%q", client.request.Header.Get("X-MBX-APIKEY"))
	}
	if client.request.URL.Query().Get("signature") == "" {
		t.Fatal("signature missing")
	}
	if strings.Contains(client.request.URL.RawQuery, "secret") {
		t.Fatal("secret leaked")
	}
	if got.AvailableBalance != "80" || got.Scope != "testnet" {
		t.Fatalf("account=%#v", got)
	}
}

func TestNewSelectsEnvironmentURL(t *testing.T) {
	testnet, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "binance", Environment: executionaccount.EnvironmentTestnet}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s"}})
	live, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "binance", Environment: executionaccount.EnvironmentLive}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s"}})
	if testnet.baseURL != testnetURL || live.baseURL != liveURL {
		t.Fatalf("urls=%s,%s", testnet.baseURL, live.baseURL)
	}
}

func TestCapabilityParsesExchangeFilters(t *testing.T) {
	client := &captureClient{body: []byte(`{"serverTime":123,"symbols":[{"symbol":"ETHUSDT","status":"TRADING","filters":[{"filterType":"LOT_SIZE","minQty":"0.001","maxQty":"100","stepSize":"0.001"},{"filterType":"MARKET_LOT_SIZE","maxQty":"50"},{"filterType":"PRICE_FILTER","tickSize":"0.01"},{"filterType":"MIN_NOTIONAL","notional":"5"}]}]}`)}
	adapter, _ := New(Options{Account: executionaccount.Account{ID: "a", Exchange: "binance", Environment: executionaccount.EnvironmentTestnet, Market: "um"}, Credential: executionaccount.Credential{APIKey: "k", APISecret: "s"}, BaseURL: "https://example.test", HTTPClient: client})
	got, err := adapter.Capability(context.Background(), "ETHUSDT")
	if err != nil {
		t.Fatal(err)
	}
	if got.QtyStep != "0.001" || got.PriceTick != "0.01" || got.MinNotional != "5" || got.MaxOrderQty != "50" {
		t.Fatalf("capability=%#v", got)
	}
}

type captureClient struct {
	request *http.Request
	body    []byte
}

func (c *captureClient) DoBytes(request *http.Request) ([]byte, error) {
	c.request = request
	return c.body, nil
}

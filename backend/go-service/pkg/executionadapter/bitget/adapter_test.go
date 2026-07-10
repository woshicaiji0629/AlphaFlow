package bitget

import (
	"alphaflow/go-service/pkg/executionaccount"
	"context"
	"net/http"
	"testing"
	"time"
)

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

type captureClient struct {
	request *http.Request
	body    []byte
}

func (c *captureClient) DoBytes(r *http.Request) ([]byte, error) { c.request = r; return c.body, nil }

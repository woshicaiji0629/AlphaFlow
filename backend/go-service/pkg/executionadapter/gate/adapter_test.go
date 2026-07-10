package gate

import (
	"alphaflow/go-service/pkg/executionaccount"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAccountSignsPrivateRequest(t *testing.T) {
	client := &captureClient{body: []byte(`{"total":"100","available":"80","unrealisedPnl":"2"}`)}
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
	if got.AvailableBalance != "80" || got.Scope != "testnet" {
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

type captureClient struct {
	request *http.Request
	body    []byte
}

func (c *captureClient) DoBytes(r *http.Request) ([]byte, error) { c.request = r; return c.body, nil }

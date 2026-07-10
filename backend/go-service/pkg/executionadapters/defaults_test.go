package executionadapters

import (
	"alphaflow/go-service/pkg/executionaccount"
	"testing"
)

func TestDefaultRegistryContainsSupportedExchanges(t *testing.T) {
	registry, err := NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, exchange := range []string{"binance", "bitget", "gate"} {
		credential := executionaccount.Credential{APIKey: "key", APISecret: "secret"}
		if exchange == "bitget" {
			credential.Passphrase = "pass"
		}
		adapter, err := registry.Build(executionaccount.Account{ID: "account", Exchange: exchange, Environment: executionaccount.EnvironmentTestnet}, credential)
		if err != nil || adapter == nil {
			t.Fatalf("Build(%s)=%#v,%v", exchange, adapter, err)
		}
	}
}

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
	for _, exchange := range []string{"binance", "bitget", "gate", "weex", "deepcoin", "hotcoin"} {
		credential := executionaccount.Credential{APIKey: "key", APISecret: "secret"}
		if exchange == "bitget" || exchange == "weex" || exchange == "deepcoin" {
			credential.Passphrase = "pass"
		}
		environment := executionaccount.EnvironmentTestnet
		if exchange == "deepcoin" || exchange == "hotcoin" {
			environment = executionaccount.EnvironmentLive
		}
		adapter, err := registry.Build(executionaccount.Account{ID: "account", Exchange: exchange, Environment: environment}, credential)
		if err != nil || adapter == nil {
			t.Fatalf("Build(%s)=%#v,%v", exchange, adapter, err)
		}
	}
}

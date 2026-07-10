package executionadapter

import (
	"alphaflow/go-service/pkg/executionaccount"
	"testing"
)

func TestEnsureTradingEnabledRequiresLiveConfirmation(t *testing.T) {
	a := executionaccount.Account{ID: "a", Exchange: "binance", Environment: executionaccount.EnvironmentLive, Enabled: true, TradingEnabled: true}
	if err := EnsureTradingEnabled(a); err == nil {
		t.Fatal("error=nil")
	}
	a.LiveConfirmed = true
	if err := EnsureTradingEnabled(a); err != nil {
		t.Fatal(err)
	}
}
func TestClientOrderIDIsStableAndBounded(t *testing.T) {
	left := ClientOrderID("af-", "very:long:intent:id:that:cannot:fit", 20)
	right := ClientOrderID("af-", "very:long:intent:id:that:cannot:fit", 20)
	if left != right || len(left) > 20 {
		t.Fatalf("ids=%q,%q", left, right)
	}
}

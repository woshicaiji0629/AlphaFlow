package executionaccount

import "testing"

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := c.Encrypt(Credential{APIKey: "key", APISecret: "secret", Passphrase: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if got.APISecret != "secret" || got.Passphrase != "pass" {
		t.Fatalf("credential=%#v", got)
	}
}
func TestLiveTradingRequiresConfirmation(t *testing.T) {
	account := Account{ID: "a", Exchange: "binance", Environment: EnvironmentLive, TradingEnabled: true}
	if err := account.Validate(); err == nil {
		t.Fatal("Validate() error=nil")
	}
}
func TestBitgetRequiresPassphrase(t *testing.T) {
	if err := (Credential{APIKey: "k", APISecret: "s"}).Validate("bitget"); err == nil {
		t.Fatal("Validate() error=nil")
	}
}

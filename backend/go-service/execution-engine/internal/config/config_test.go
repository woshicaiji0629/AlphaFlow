package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPaperConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "execution.toml")
	if err := os.WriteFile(path, []byte("[execution]\nmode='paper'\n[nats]\nintent_subject='execution.intent'\nreport_subject='execution.report'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NATS.IntentSubject != "execution.intent" {
		t.Fatalf("config = %#v", cfg)
	}
}
func TestLoadRejectsLiveModeWithoutAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "execution.toml")
	if err := os.WriteFile(path, []byte("[execution]\nmode='live'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want live disabled")
	}
}
func TestLoadTestnetAccountFromEnvironment(t *testing.T) {
	t.Setenv("WEEX_KEY", "key")
	t.Setenv("WEEX_SECRET", "secret")
	t.Setenv("WEEX_PASS", "pass")
	path := filepath.Join(t.TempDir(), "execution.toml")
	content := "[execution]\nmode='testnet'\n[[accounts]]\nid='demo'\nexchange='weex'\nenvironment='testnet'\nmarket='um'\nposition_mode='hedge'\nmargin_mode='cross'\nenabled=true\ntrading_enabled=true\napi_key_env='WEEX_KEY'\napi_secret_env='WEEX_SECRET'\npassphrase_env='WEEX_PASS'\nsymbols=['BTCUSDT']\nstrategies=['supertrend']\nmargin_quote=100\nleverage=10\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	account, credential, err := cfg.Accounts[0].Build()
	if err != nil || account.ID != "demo" || credential.APIKey != "key" || len(cfg.Accounts[0].Symbols) != 1 {
		t.Fatalf("account=%#v credential=%#v err=%v", account, credential, err)
	}
}

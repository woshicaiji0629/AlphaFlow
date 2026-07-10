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
func TestLoadRejectsLiveMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "execution.toml")
	if err := os.WriteFile(path, []byte("[execution]\nmode='live'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want live disabled")
	}
}

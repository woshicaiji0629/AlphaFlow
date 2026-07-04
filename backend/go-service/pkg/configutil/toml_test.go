package configutil

import (
	"os"
	"path/filepath"
	"testing"
)

type sampleConfig struct {
	Runtime sampleRuntimeConfig `toml:"runtime"`
}

type sampleRuntimeConfig struct {
	Interval string `toml:"interval"`
}

func TestDecodeTOMLFileStrictRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[runtime]
interval = "1s"
unknown = true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var cfg sampleConfig
	if err := DecodeTOMLFileStrict(path, &cfg); err == nil {
		t.Fatal("DecodeTOMLFileStrict() error = nil, want unknown field error")
	}
}

func TestDecodeTOMLFileStrictAcceptsKnownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[runtime]
interval = "1s"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var cfg sampleConfig
	if err := DecodeTOMLFileStrict(path, &cfg); err != nil {
		t.Fatalf("DecodeTOMLFileStrict() error = %v", err)
	}
	if cfg.Runtime.Interval != "1s" {
		t.Fatalf("interval = %q, want 1s", cfg.Runtime.Interval)
	}
}

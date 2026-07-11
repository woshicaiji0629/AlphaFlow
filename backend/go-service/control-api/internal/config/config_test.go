package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	path := writeConfig(t, `
[http]
addr = "127.0.0.1:9090"
mode = "test"
read_timeout = "2s"
write_timeout = "3s"
shutdown_timeout = "4s"
allowed_origins = ["http://localhost:5173"]
max_body_bytes = 1048576

[postgres]
dsn = "postgres://example"
max_conns = 8
min_conns = 2
connect_timeout = "5s"
auto_migrate = false
migration_timeout = "20s"

[session]
cookie_name = "session"
csrf_cookie_name = "csrf"
secure_cookie = false
idle_timeout = "1h"
absolute_timeout = "24h"
refresh_interval = "5m"

[redis]
addr = "localhost:6380"
password = ""
db = 0
pool_size = 10
min_idle_conns = 2

[login_limit]
window = "15m"
max_email_attempts = 5
max_ip_attempts = 50
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.Addr != "127.0.0.1:9090" || cfg.Postgres.DSN != "postgres://example" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if cfg.Postgres.MaxConns != 8 || cfg.Postgres.MinConns != 2 {
		t.Fatalf("unexpected postgres pool config: %#v", cfg.Postgres)
	}
	if got, err := ShutdownTimeout(cfg); err != nil || got != 4*time.Second {
		t.Fatalf("ShutdownTimeout() = %v, %v", got, err)
	}
}

func TestLoadRejectsMissingPath(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatal("Load() error = nil, want required path error")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := writeConfig(t, "[postgres]\ndsn = \"postgres://example\"\nunknown = true\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown fields") {
		t.Fatalf("Load() error = %v, want unknown fields error", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control-api.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

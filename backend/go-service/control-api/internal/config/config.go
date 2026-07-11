package config

import (
	"fmt"
	"strings"
	"time"

	"alphaflow/go-service/pkg/configutil"
)

type Config struct {
	HTTP       HTTPConfig       `toml:"http"`
	Postgres   PostgresConfig   `toml:"postgres"`
	Session    SessionConfig    `toml:"session"`
	Redis      RedisConfig      `toml:"redis"`
	LoginLimit LoginLimitConfig `toml:"login_limit"`
}

type RedisConfig struct {
	Addr         string `toml:"addr"`
	Password     string `toml:"password"`
	DB           int    `toml:"db"`
	PoolSize     int    `toml:"pool_size"`
	MinIdleConns int    `toml:"min_idle_conns"`
}
type LoginLimitConfig struct {
	Window           string `toml:"window"`
	MaxEmailAttempts int    `toml:"max_email_attempts"`
	MaxIPAttempts    int    `toml:"max_ip_attempts"`
}

type SessionConfig struct {
	CookieName      string `toml:"cookie_name"`
	CSRFCookieName  string `toml:"csrf_cookie_name"`
	SecureCookie    bool   `toml:"secure_cookie"`
	IdleTimeout     string `toml:"idle_timeout"`
	AbsoluteTimeout string `toml:"absolute_timeout"`
	RefreshInterval string `toml:"refresh_interval"`
}

type HTTPConfig struct {
	Addr            string   `toml:"addr"`
	Mode            string   `toml:"mode"`
	ReadTimeout     string   `toml:"read_timeout"`
	WriteTimeout    string   `toml:"write_timeout"`
	ShutdownTimeout string   `toml:"shutdown_timeout"`
	AllowedOrigins  []string `toml:"allowed_origins"`
	MaxBodyBytes    int64    `toml:"max_body_bytes"`
}

type PostgresConfig struct {
	DSN              string `toml:"dsn"`
	MaxConns         int32  `toml:"max_conns"`
	MinConns         int32  `toml:"min_conns"`
	ConnectTimeout   string `toml:"connect_timeout"`
	AutoMigrate      bool   `toml:"auto_migrate"`
	MigrationTimeout string `toml:"migration_timeout"`
}

func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, fmt.Errorf("control-api config path is required")
	}
	cfg := defaultConfig()
	if err := configutil.DecodeTOMLFileStrict(path, &cfg); err != nil {
		return Config{}, err
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaultConfig() Config {
	return Config{HTTP: HTTPConfig{
		Addr:            "127.0.0.1:8080",
		Mode:            "debug",
		ReadTimeout:     "10s",
		WriteTimeout:    "30s",
		ShutdownTimeout: "10s",
		AllowedOrigins:  []string{"http://localhost:3000"},
		MaxBodyBytes:    1 << 20,
	}, Postgres: PostgresConfig{
		MaxConns:         10,
		MinConns:         1,
		ConnectTimeout:   "5s",
		MigrationTimeout: "30s",
	}, Session: SessionConfig{
		CookieName:      "alphaflow_session",
		CSRFCookieName:  "alphaflow_csrf",
		SecureCookie:    true,
		IdleTimeout:     "12h",
		AbsoluteTimeout: "168h",
		RefreshInterval: "5m",
	}, Redis: RedisConfig{Addr: "localhost:6380", PoolSize: 10, MinIdleConns: 2}, LoginLimit: LoginLimitConfig{
		Window: "15m", MaxEmailAttempts: 5, MaxIPAttempts: 50,
	}}
}

func validate(cfg Config) error {
	if strings.TrimSpace(cfg.HTTP.Addr) == "" {
		return fmt.Errorf("http.addr is required")
	}
	if cfg.HTTP.Mode != "debug" && cfg.HTTP.Mode != "release" && cfg.HTTP.Mode != "test" {
		return fmt.Errorf("http.mode must be debug, release, or test")
	}
	if cfg.HTTP.MaxBodyBytes <= 0 {
		return fmt.Errorf("http.max_body_bytes must be positive")
	}
	if len(cfg.HTTP.AllowedOrigins) == 0 {
		return fmt.Errorf("http.allowed_origins must not be empty")
	}
	for _, origin := range cfg.HTTP.AllowedOrigins {
		if !validOrigin(origin) {
			return fmt.Errorf("invalid http.allowed_origins value %q", origin)
		}
	}
	if strings.TrimSpace(cfg.Postgres.DSN) == "" {
		return fmt.Errorf("postgres.dsn is required")
	}
	if cfg.Postgres.MaxConns <= 0 {
		return fmt.Errorf("postgres.max_conns must be positive")
	}
	if cfg.Postgres.MinConns < 0 || cfg.Postgres.MinConns > cfg.Postgres.MaxConns {
		return fmt.Errorf("postgres.min_conns must be between zero and max_conns")
	}
	if strings.TrimSpace(cfg.Session.CookieName) == "" || strings.TrimSpace(cfg.Session.CSRFCookieName) == "" {
		return fmt.Errorf("session cookie names are required")
	}
	if cfg.Session.CookieName == cfg.Session.CSRFCookieName {
		return fmt.Errorf("session cookie names must be different")
	}
	if strings.TrimSpace(cfg.Redis.Addr) == "" {
		return fmt.Errorf("redis.addr is required")
	}
	if cfg.LoginLimit.MaxEmailAttempts <= 0 || cfg.LoginLimit.MaxIPAttempts <= 0 {
		return fmt.Errorf("login limit attempts must be positive")
	}
	for name, raw := range map[string]string{
		"http.read_timeout":          cfg.HTTP.ReadTimeout,
		"http.write_timeout":         cfg.HTTP.WriteTimeout,
		"http.shutdown_timeout":      cfg.HTTP.ShutdownTimeout,
		"postgres.connect_timeout":   cfg.Postgres.ConnectTimeout,
		"postgres.migration_timeout": cfg.Postgres.MigrationTimeout,
		"session.idle_timeout":       cfg.Session.IdleTimeout,
		"session.absolute_timeout":   cfg.Session.AbsoluteTimeout,
		"session.refresh_interval":   cfg.Session.RefreshInterval,
		"login_limit.window":         cfg.LoginLimit.Window,
	} {
		if _, err := parsePositiveDuration(name, raw); err != nil {
			return err
		}
	}
	return nil
}

func LoginLimitWindow(cfg Config) (time.Duration, error) {
	return parsePositiveDuration("login_limit.window", cfg.LoginLimit.Window)
}

func validOrigin(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func SessionIdleTimeout(cfg Config) (time.Duration, error) {
	return parsePositiveDuration("session.idle_timeout", cfg.Session.IdleTimeout)
}

func SessionAbsoluteTimeout(cfg Config) (time.Duration, error) {
	return parsePositiveDuration("session.absolute_timeout", cfg.Session.AbsoluteTimeout)
}

func SessionRefreshInterval(cfg Config) (time.Duration, error) {
	return parsePositiveDuration("session.refresh_interval", cfg.Session.RefreshInterval)
}

func PostgresConnectTimeout(cfg Config) (time.Duration, error) {
	return parsePositiveDuration("postgres.connect_timeout", cfg.Postgres.ConnectTimeout)
}

func MigrationTimeout(cfg Config) (time.Duration, error) {
	return parsePositiveDuration("postgres.migration_timeout", cfg.Postgres.MigrationTimeout)
}

func ReadTimeout(cfg Config) (time.Duration, error) {
	return parsePositiveDuration("http.read_timeout", cfg.HTTP.ReadTimeout)
}

func WriteTimeout(cfg Config) (time.Duration, error) {
	return parsePositiveDuration("http.write_timeout", cfg.HTTP.WriteTimeout)
}

func ShutdownTimeout(cfg Config) (time.Duration, error) {
	return parsePositiveDuration("http.shutdown_timeout", cfg.HTTP.ShutdownTimeout)
}

func parsePositiveDuration(name, raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return d, nil
}

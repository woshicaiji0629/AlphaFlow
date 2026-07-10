package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"alphaflow/go-service/pkg/configutil"
	"alphaflow/go-service/pkg/executionaccount"
	"alphaflow/go-service/pkg/executionbus"
)

type Config struct {
	NATS      NATS      `toml:"nats"`
	Redis     Redis     `toml:"redis"`
	Execution Execution `toml:"execution"`
	Accounts  []Account `toml:"accounts"`
}
type NATS struct {
	URL           string `toml:"url"`
	Stream        string `toml:"stream"`
	IntentSubject string `toml:"intent_subject"`
	ReportSubject string `toml:"report_subject"`
	Durable       string `toml:"durable"`
	Block         string `toml:"block"`
	AckWait       string `toml:"ack_wait"`
	Batch         int    `toml:"batch"`
}
type Redis struct {
	Addr         string `toml:"addr"`
	Password     string `toml:"password"`
	DB           int    `toml:"db"`
	PoolSize     int    `toml:"pool_size"`
	MinIdleConns int    `toml:"min_idle_conns"`
}
type Execution struct {
	Mode       string `toml:"mode"`
	PaperPrice string `toml:"paper_price"`
}
type Account struct {
	ID, Name, Exchange, Environment, Market string
	Symbols                                 []string          `toml:"symbols"`
	Strategies                              []string          `toml:"strategies"`
	SymbolMap                               map[string]string `toml:"symbol_map"`
	PositionMode                            string            `toml:"position_mode"`
	MarginMode                              string            `toml:"margin_mode"`
	Enabled                                 bool              `toml:"enabled"`
	TradingEnabled                          bool              `toml:"trading_enabled"`
	LiveConfirmed                           bool              `toml:"live_confirmed"`
	APIKeyEnv                               string            `toml:"api_key_env"`
	APISecretEnv                            string            `toml:"api_secret_env"`
	PassphraseEnv                           string            `toml:"passphrase_env"`
	MarginQuote                             float64           `toml:"margin_quote"`
	AllocationPct                           float64           `toml:"allocation_pct"`
	Leverage                                float64           `toml:"leverage"`
	MaxPositionNotional                     float64           `toml:"max_position_notional"`
	MaxMarginUsagePct                       float64           `toml:"max_margin_usage_pct"`
	DisableShort                            bool              `toml:"disable_short"`
}

func Load(path string) (Config, error) {
	cfg := Config{NATS: NATS{URL: "nats://localhost:4222", Stream: executionbus.DefaultStream, IntentSubject: executionbus.DefaultIntentSubject, ReportSubject: executionbus.DefaultReportSubject, Durable: "execution-engine", Block: "1s", AckWait: "30s", Batch: 10}, Redis: Redis{Addr: "localhost:6380", PoolSize: 20, MinIdleConns: 5}, Execution: Execution{Mode: "paper"}}
	if err := configutil.DecodeTOMLFileStrict(path, &cfg); err != nil {
		return Config{}, err
	}
	cfg.NATS.URL = envOrValue("ALPHAFLOW_NATS_URL", cfg.NATS.URL)
	cfg.Redis.Addr = envOrValue("ALPHAFLOW_REDIS_ADDR", cfg.Redis.Addr)
	cfg.Redis.Password = envOrValue("ALPHAFLOW_REDIS_PASSWORD", cfg.Redis.Password)
	if cfg.Execution.Mode != "paper" && cfg.Execution.Mode != "testnet" && cfg.Execution.Mode != "live" {
		return Config{}, fmt.Errorf("unsupported execution mode %q", cfg.Execution.Mode)
	}
	if cfg.Execution.Mode != "paper" && len(cfg.Accounts) == 0 {
		return Config{}, fmt.Errorf("execution mode %s requires accounts", cfg.Execution.Mode)
	}
	seen := map[string]struct{}{}
	enabledAccounts := 0
	for _, account := range cfg.Accounts {
		if !account.Enabled {
			continue
		}
		enabledAccounts++
		model, credential, err := account.Build()
		if err != nil {
			return Config{}, fmt.Errorf("account %s: %w", account.ID, err)
		}
		if string(model.Environment) != cfg.Execution.Mode {
			return Config{}, fmt.Errorf("account %s environment %s does not match execution mode %s", account.ID, model.Environment, cfg.Execution.Mode)
		}
		key := model.Exchange + ":" + model.ID
		if _, ok := seen[key]; ok {
			return Config{}, fmt.Errorf("duplicate account %s", key)
		}
		seen[key] = struct{}{}
		if err := credential.Validate(model.Exchange); err != nil {
			return Config{}, fmt.Errorf("account %s: %w", account.ID, err)
		}
		if account.MarginQuote <= 0 && account.AllocationPct <= 0 {
			return Config{}, fmt.Errorf("account %s requires margin_quote or allocation_pct", account.ID)
		}
		if account.MarginQuote > 0 && account.AllocationPct > 0 {
			return Config{}, fmt.Errorf("account %s cannot set both margin_quote and allocation_pct", account.ID)
		}
		if account.AllocationPct < 0 || account.AllocationPct > 1 {
			return Config{}, fmt.Errorf("account %s allocation_pct must be between 0 and 1", account.ID)
		}
		if account.MaxMarginUsagePct < 0 || account.MaxMarginUsagePct > 1 {
			return Config{}, fmt.Errorf("account %s max_margin_usage_pct must be between 0 and 1", account.ID)
		}
		if account.MaxPositionNotional < 0 {
			return Config{}, fmt.Errorf("account %s max_position_notional cannot be negative", account.ID)
		}
		if account.Leverage <= 0 {
			return Config{}, fmt.Errorf("account %s leverage must be positive", account.ID)
		}
		if len(account.Strategies) == 0 {
			return Config{}, fmt.Errorf("account %s strategies cannot be empty", account.ID)
		}
	}
	if cfg.Execution.Mode != "paper" && enabledAccounts == 0 {
		return Config{}, fmt.Errorf("execution mode %s requires at least one enabled account", cfg.Execution.Mode)
	}
	return cfg, nil
}
func (a Account) Build() (executionaccount.Account, executionaccount.Credential, error) {
	model := executionaccount.Account{ID: strings.TrimSpace(a.ID), Name: a.Name, Exchange: strings.ToLower(strings.TrimSpace(a.Exchange)), Environment: executionaccount.Environment(strings.ToLower(a.Environment)), Market: a.Market, PositionMode: executionaccount.PositionMode(a.PositionMode), MarginMode: executionaccount.MarginMode(a.MarginMode), Enabled: a.Enabled, TradingEnabled: a.TradingEnabled, LiveConfirmed: a.LiveConfirmed}
	if err := model.Validate(); err != nil {
		return executionaccount.Account{}, executionaccount.Credential{}, err
	}
	if strings.TrimSpace(a.APIKeyEnv) == "" || strings.TrimSpace(a.APISecretEnv) == "" {
		return executionaccount.Account{}, executionaccount.Credential{}, fmt.Errorf("api_key_env and api_secret_env are required")
	}
	credential := executionaccount.Credential{APIKey: os.Getenv(a.APIKeyEnv), APISecret: os.Getenv(a.APISecretEnv)}
	if a.PassphraseEnv != "" {
		credential.Passphrase = os.Getenv(a.PassphraseEnv)
	}
	return model, credential, nil
}
func Block(cfg Config) (time.Duration, error)   { return time.ParseDuration(cfg.NATS.Block) }
func AckWait(cfg Config) (time.Duration, error) { return time.ParseDuration(cfg.NATS.AckWait) }
func envOrValue(name, value string) string {
	if candidate := strings.TrimSpace(os.Getenv(name)); candidate != "" {
		return candidate
	}
	return strings.TrimSpace(value)
}

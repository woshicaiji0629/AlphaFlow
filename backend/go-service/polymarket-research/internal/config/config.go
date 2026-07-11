package config

import (
	"fmt"
	"strings"
	"time"

	"alphaflow/go-service/pkg/configutil"
)

type Config struct {
	Gamma      Gamma      `toml:"gamma"`
	Research   Research   `toml:"research"`
	Realtime   Realtime   `toml:"realtime"`
	Batch      Batch      `toml:"batch"`
	ClickHouse ClickHouse `toml:"clickhouse"`
}

type Realtime struct {
	CLOBURL       string `toml:"clob_url"`
	RTDSURL       string `toml:"rtds_url"`
	ReconnectWait string `toml:"reconnect_wait"`
}
type Batch struct {
	MaxSize        int    `toml:"max_size"`
	FlushInterval  string `toml:"flush_interval"`
	ChannelSize    int    `toml:"channel_size"`
	HealthInterval string `toml:"health_interval"`
}

type Gamma struct {
	BaseURL      string `toml:"base_url"`
	PollInterval string `toml:"poll_interval"`
	PageSize     int    `toml:"page_size"`
}

type Research struct {
	Symbols   []string `toml:"symbols"`
	Durations []string `toml:"durations"`
}

type ClickHouse struct {
	Enabled     bool   `toml:"enabled"`
	Addr        string `toml:"addr"`
	Database    string `toml:"database"`
	Username    string `toml:"username"`
	Password    string `toml:"password"`
	DialTimeout string `toml:"dial_timeout"`
	ReadTimeout string `toml:"read_timeout"`
}

func Load(path string) (Config, error) {
	cfg := Config{
		Gamma:      Gamma{BaseURL: "https://gamma-api.polymarket.com", PollInterval: "15s", PageSize: 100},
		Research:   Research{Symbols: []string{"BTC", "ETH", "SOL", "XRP", "DOGE", "BNB", "HYPE"}, Durations: []string{"5m", "15m"}},
		Realtime:   Realtime{CLOBURL: "wss://ws-subscriptions-clob.polymarket.com/ws/market", RTDSURL: "wss://ws-live-data.polymarket.com", ReconnectWait: "2s"},
		Batch:      Batch{MaxSize: 500, FlushInterval: "1s", ChannelSize: 10000, HealthInterval: "30s"},
		ClickHouse: ClickHouse{Addr: "localhost:9000", Database: "alphaflow", Username: "default", DialTimeout: "5s", ReadTimeout: "30s"},
	}
	if strings.TrimSpace(path) == "" {
		return Config{}, fmt.Errorf("config path is required")
	}
	if err := configutil.DecodeTOMLFileStrict(path, &cfg); err != nil {
		return Config{}, err
	}
	cfg.Gamma.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.Gamma.BaseURL), "/")
	if cfg.Gamma.BaseURL == "" || cfg.Gamma.PageSize <= 0 || cfg.Gamma.PageSize > 500 {
		return Config{}, fmt.Errorf("gamma base_url and page_size between 1 and 500 are required")
	}
	if _, err := PollInterval(cfg); err != nil {
		return Config{}, err
	}
	durations := map[string]struct{}{}
	for index, duration := range cfg.Research.Durations {
		duration = strings.ToLower(strings.TrimSpace(duration))
		if duration != "5m" && duration != "15m" {
			return Config{}, fmt.Errorf("unsupported research duration %q", duration)
		}
		if _, ok := durations[duration]; ok {
			return Config{}, fmt.Errorf("duplicate research duration %q", duration)
		}
		durations[duration] = struct{}{}
		cfg.Research.Durations[index] = duration
	}
	if len(cfg.Research.Durations) == 0 {
		return Config{}, fmt.Errorf("at least one research duration is required")
	}
	seen := map[string]struct{}{}
	for index, symbol := range cfg.Research.Symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if !supportedSymbol(symbol) {
			return Config{}, fmt.Errorf("unsupported research symbol %q", symbol)
		}
		if _, ok := seen[symbol]; ok {
			return Config{}, fmt.Errorf("duplicate research symbol %q", symbol)
		}
		seen[symbol] = struct{}{}
		cfg.Research.Symbols[index] = symbol
	}
	if len(cfg.Research.Symbols) == 0 {
		return Config{}, fmt.Errorf("at least one research symbol is required")
	}
	if strings.TrimSpace(cfg.Realtime.CLOBURL) == "" || strings.TrimSpace(cfg.Realtime.RTDSURL) == "" {
		return Config{}, fmt.Errorf("realtime websocket urls are required")
	}
	if _, err := ReconnectWait(cfg); err != nil {
		return Config{}, err
	}
	if cfg.Batch.MaxSize <= 0 || cfg.Batch.ChannelSize <= 0 {
		return Config{}, fmt.Errorf("batch max_size and channel_size must be positive")
	}
	if _, err := BatchFlushInterval(cfg); err != nil {
		return Config{}, err
	}
	if _, err := HealthInterval(cfg); err != nil {
		return Config{}, err
	}
	if cfg.ClickHouse.Enabled {
		if strings.TrimSpace(cfg.ClickHouse.Addr) == "" || strings.TrimSpace(cfg.ClickHouse.Database) == "" {
			return Config{}, fmt.Errorf("clickhouse addr and database are required when enabled")
		}
		if _, err := ClickHouseDialTimeout(cfg); err != nil {
			return Config{}, err
		}
		if _, err := ClickHouseReadTimeout(cfg); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func supportedSymbol(symbol string) bool {
	switch symbol {
	case "BTC", "ETH", "SOL", "XRP", "DOGE", "BNB", "HYPE":
		return true
	default:
		return false
	}
}

func PollInterval(cfg Config) (time.Duration, error) {
	value, err := time.ParseDuration(cfg.Gamma.PollInterval)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid gamma poll_interval %q", cfg.Gamma.PollInterval)
	}
	return value, nil
}
func ClickHouseDialTimeout(cfg Config) (time.Duration, error) {
	return time.ParseDuration(cfg.ClickHouse.DialTimeout)
}
func ClickHouseReadTimeout(cfg Config) (time.Duration, error) {
	return time.ParseDuration(cfg.ClickHouse.ReadTimeout)
}
func ReconnectWait(cfg Config) (time.Duration, error) {
	value, err := time.ParseDuration(cfg.Realtime.ReconnectWait)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid realtime reconnect_wait %q", cfg.Realtime.ReconnectWait)
	}
	return value, nil
}
func BatchFlushInterval(cfg Config) (time.Duration, error) {
	v, e := time.ParseDuration(cfg.Batch.FlushInterval)
	if e != nil || v <= 0 {
		return 0, fmt.Errorf("invalid batch flush_interval %q", cfg.Batch.FlushInterval)
	}
	return v, nil
}
func HealthInterval(cfg Config) (time.Duration, error) {
	v, e := time.ParseDuration(cfg.Batch.HealthInterval)
	if e != nil || v <= 0 {
		return 0, fmt.Errorf("invalid batch health_interval %q", cfg.Batch.HealthInterval)
	}
	return v, nil
}

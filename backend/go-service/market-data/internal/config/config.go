package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Binance   BinanceConfig   `toml:"binance"`
	OKX       OKXConfig       `toml:"okx"`
	Gate      GateConfig      `toml:"gate"`
	Logging   LoggingConfig   `toml:"logging"`
	WebSocket WebSocketConfig `toml:"websocket"`
	Retention RetentionConfig `toml:"retention"`
}

type BinanceConfig struct {
	Enabled  bool     `toml:"enabled"`
	RESTBase string   `toml:"rest_base"`
	WSBase   string   `toml:"ws_base"`
	Symbols  []string `toml:"symbols"`
}

type OKXConfig struct {
	Enabled  bool     `toml:"enabled"`
	RESTBase string   `toml:"rest_base"`
	WSBase   string   `toml:"ws_base"`
	Symbols  []string `toml:"symbols"`
}

type GateConfig struct {
	Enabled  bool     `toml:"enabled"`
	RESTBase string   `toml:"rest_base"`
	WSBase   string   `toml:"ws_base"`
	Settle   string   `toml:"settle"`
	Symbols  []string `toml:"symbols"`
}

type LoggingConfig struct {
	Service    string `toml:"service"`
	Level      string `toml:"level"`
	Format     string `toml:"format"`
	Output     string `toml:"output"`
	Dir        string `toml:"dir"`
	Filename   string `toml:"filename"`
	MaxSizeMB  int    `toml:"max_size_mb"`
	MaxBackups int    `toml:"max_backups"`
	MaxAgeDays int    `toml:"max_age_days"`
	Compress   bool   `toml:"compress"`
}

type WebSocketConfig struct {
	ReconnectDelay string `toml:"reconnect_delay"`
}

type RetentionConfig struct {
	KlineLimit       int64  `toml:"kline_limit"`
	LiquidationLimit int64  `toml:"liquidation_limit"`
	LatestTTL        string `toml:"latest_ttl"`
	PollingTTL       string `toml:"polling_ttl"`
}

func Load(configPath string) (Config, error) {
	path := resolvePath(configPath)
	cfg := defaultConfig()

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %s: %w", path, err)
	}

	normalize(&cfg)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		Binance: BinanceConfig{
			Enabled:  true,
			RESTBase: "https://fapi.binance.com",
			WSBase:   "wss://fstream.binance.com",
			Symbols:  []string{"ETHUSDT"},
		},
		OKX: OKXConfig{
			Enabled:  false,
			RESTBase: "https://www.okx.com",
			WSBase:   "wss://ws.okx.com:8443/ws/v5/public",
			Symbols:  []string{"ETH-USDT-SWAP"},
		},
		Gate: GateConfig{
			Enabled:  false,
			RESTBase: "https://api.gateio.ws/api/v4",
			WSBase:   "wss://fx-ws.gateio.ws/v4/ws/usdt",
			Settle:   "usdt",
			Symbols:  []string{"ETH_USDT"},
		},
		Logging: LoggingConfig{
			Service:    "market-data",
			Level:      "info",
			Format:     "json",
			Output:     "stdout",
			Dir:        "logs",
			Filename:   "market-data.log",
			MaxSizeMB:  100,
			MaxBackups: 10,
			MaxAgeDays: 30,
			Compress:   true,
		},
		WebSocket: WebSocketConfig{
			ReconnectDelay: "5s",
		},
		Retention: RetentionConfig{
			KlineLimit:       1000,
			LiquidationLimit: 200,
			LatestTTL:        "24h",
			PollingTTL:       "24h",
		},
	}
}

func validate(cfg Config) error {
	if cfg.Binance.Enabled {
		if cfg.Binance.RESTBase == "" {
			return fmt.Errorf("binance rest_base cannot be empty")
		}
		if cfg.Binance.WSBase == "" {
			return fmt.Errorf("binance ws_base cannot be empty")
		}
		if len(cfg.Binance.Symbols) == 0 {
			return fmt.Errorf("binance symbols cannot be empty when enabled")
		}
	}
	if cfg.OKX.Enabled {
		if cfg.OKX.RESTBase == "" {
			return fmt.Errorf("okx rest_base cannot be empty")
		}
		if cfg.OKX.WSBase == "" {
			return fmt.Errorf("okx ws_base cannot be empty")
		}
		if len(cfg.OKX.Symbols) == 0 {
			return fmt.Errorf("okx symbols cannot be empty when enabled")
		}
	}
	if cfg.Gate.Enabled {
		if cfg.Gate.RESTBase == "" {
			return fmt.Errorf("gate rest_base cannot be empty")
		}
		if cfg.Gate.WSBase == "" {
			return fmt.Errorf("gate ws_base cannot be empty")
		}
		if cfg.Gate.Settle == "" {
			return fmt.Errorf("gate settle cannot be empty")
		}
		if len(cfg.Gate.Symbols) == 0 {
			return fmt.Errorf("gate symbols cannot be empty when enabled")
		}
	}
	if cfg.Retention.LiquidationLimit <= 0 {
		return fmt.Errorf("liquidation_limit must be positive")
	}
	if cfg.Retention.KlineLimit <= 0 {
		return fmt.Errorf("kline_limit must be positive")
	}
	if _, err := ReconnectDelay(cfg); err != nil {
		return err
	}
	if _, err := LatestTTL(cfg); err != nil {
		return err
	}
	if _, err := PollingTTL(cfg); err != nil {
		return err
	}
	return nil
}

func RESTLimit() int {
	return 200
}

func MarkPriceInterval() string {
	return "1s"
}

func OpenInterestInterval() time.Duration {
	return time.Minute
}

func BinanceIntervals() []string {
	return []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h"}
}

func OKXIntervals() []string {
	return []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h"}
}

func GateIntervals() []string {
	return []string{"1m", "5m", "15m", "30m", "1h", "4h"}
}

func ReconnectDelay(cfg Config) (time.Duration, error) {
	if cfg.WebSocket.ReconnectDelay == "" {
		return 5 * time.Second, nil
	}
	delay, err := time.ParseDuration(cfg.WebSocket.ReconnectDelay)
	if err != nil {
		return 0, fmt.Errorf("parse reconnect_delay: %w", err)
	}
	if delay <= 0 {
		return 0, fmt.Errorf("reconnect_delay must be positive")
	}
	return delay, nil
}

func LatestTTL(cfg Config) (time.Duration, error) {
	return parseTTL("latest_ttl", cfg.Retention.LatestTTL, 24*time.Hour)
}

func PollingTTL(cfg Config) (time.Duration, error) {
	return parseTTL("polling_ttl", cfg.Retention.PollingTTL, 24*time.Hour)
}

func parseTTL(name string, raw string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}
	ttl, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if ttl <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return ttl, nil
}

func resolvePath(configPath string) string {
	value := strings.TrimSpace(configPath)
	if value == "" {
		value = strings.TrimSpace(os.Getenv("ALPHAFLOW_MARKET_CONFIG"))
	}
	if value == "" {
		value = "configs/local.toml"
	}
	return filepath.Clean(value)
}

func normalize(cfg *Config) {
	for index, symbol := range cfg.Binance.Symbols {
		cfg.Binance.Symbols[index] = strings.ToUpper(strings.TrimSpace(symbol))
	}
	for index, symbol := range cfg.OKX.Symbols {
		cfg.OKX.Symbols[index] = strings.ToUpper(strings.TrimSpace(symbol))
	}
	cfg.Gate.Settle = strings.ToLower(strings.TrimSpace(cfg.Gate.Settle))
	for index, symbol := range cfg.Gate.Symbols {
		cfg.Gate.Symbols[index] = strings.ToUpper(strings.TrimSpace(symbol))
	}
}

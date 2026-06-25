package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Binance   BinanceConfig   `toml:"binance"`
	Gate      GateConfig      `toml:"gate"`
	Bitget    BitgetConfig    `toml:"bitget"`
	Bybit     BybitConfig     `toml:"bybit"`
	Logging   LoggingConfig   `toml:"logging"`
	WebSocket WebSocketConfig `toml:"websocket"`
}

type BinanceConfig struct {
	Enabled bool     `toml:"enabled"`
	Symbols []string `toml:"symbols"`
}

type GateConfig struct {
	Enabled bool     `toml:"enabled"`
	Symbols []string `toml:"symbols"`
}

type BitgetConfig struct {
	Enabled bool     `toml:"enabled"`
	Symbols []string `toml:"symbols"`
}

type BybitConfig struct {
	Enabled bool     `toml:"enabled"`
	Symbols []string `toml:"symbols"`
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
			Enabled: true,
			Symbols: []string{"ETHUSDT"},
		},
		Gate: GateConfig{
			Enabled: false,
			Symbols: []string{"ETH_USDT"},
		},
		Bitget: BitgetConfig{
			Enabled: false,
			Symbols: []string{"ETHUSDT"},
		},
		Bybit: BybitConfig{
			Enabled: false,
			Symbols: []string{"ETHUSDT"},
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
	}
}

func validate(cfg Config) error {
	if cfg.Binance.Enabled {
		if len(cfg.Binance.Symbols) == 0 {
			return fmt.Errorf("binance symbols cannot be empty when enabled")
		}
	}
	if cfg.Gate.Enabled {
		if len(cfg.Gate.Symbols) == 0 {
			return fmt.Errorf("gate symbols cannot be empty when enabled")
		}
	}
	if cfg.Bitget.Enabled {
		if len(cfg.Bitget.Symbols) == 0 {
			return fmt.Errorf("bitget symbols cannot be empty when enabled")
		}
	}
	if cfg.Bybit.Enabled {
		if len(cfg.Bybit.Symbols) == 0 {
			return fmt.Errorf("bybit symbols cannot be empty when enabled")
		}
	}
	if _, err := ReconnectDelay(cfg); err != nil {
		return err
	}
	return nil
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
	for index, symbol := range cfg.Gate.Symbols {
		cfg.Gate.Symbols[index] = strings.ToUpper(strings.TrimSpace(symbol))
	}
	for index, symbol := range cfg.Bitget.Symbols {
		cfg.Bitget.Symbols[index] = strings.ToUpper(strings.TrimSpace(symbol))
	}
	for index, symbol := range cfg.Bybit.Symbols {
		cfg.Bybit.Symbols[index] = strings.ToUpper(strings.TrimSpace(symbol))
	}
}

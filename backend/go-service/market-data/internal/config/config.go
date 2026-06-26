package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Binance    BinanceConfig    `toml:"binance"`
	Gate       GateConfig       `toml:"gate"`
	Bitget     BitgetConfig     `toml:"bitget"`
	Bybit      BybitConfig      `toml:"bybit"`
	ClickHouse ClickHouseConfig `toml:"clickhouse"`
	Logging    LoggingConfig    `toml:"logging"`
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

type ClickHouseConfig struct {
	Enabled       bool   `toml:"enabled"`
	Addr          string `toml:"addr"`
	Database      string `toml:"database"`
	Username      string `toml:"username"`
	Password      string `toml:"password"`
	DialTimeout   string `toml:"dial_timeout"`
	ReadTimeout   string `toml:"read_timeout"`
	RetryInterval string `toml:"retry_interval"`
	RetryBatch    int    `toml:"retry_batch"`
	MaxPending    int64  `toml:"max_pending"`
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
		ClickHouse: ClickHouseConfig{
			Enabled:       false,
			Addr:          "localhost:9000",
			Database:      "alphaflow",
			Username:      "default",
			Password:      "",
			DialTimeout:   "5s",
			ReadTimeout:   "30s",
			RetryInterval: "10s",
			RetryBatch:    100,
			MaxPending:    100000,
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
	if cfg.ClickHouse.Enabled {
		if strings.TrimSpace(cfg.ClickHouse.Addr) == "" {
			return fmt.Errorf("clickhouse addr cannot be empty when enabled")
		}
		if strings.TrimSpace(cfg.ClickHouse.Database) == "" {
			return fmt.Errorf("clickhouse database cannot be empty when enabled")
		}
		if _, err := ClickHouseDialTimeout(cfg); err != nil {
			return err
		}
		if _, err := ClickHouseReadTimeout(cfg); err != nil {
			return err
		}
		if _, err := ClickHouseRetryInterval(cfg); err != nil {
			return err
		}
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
	cfg.ClickHouse.Addr = envOrValue("ALPHAFLOW_CLICKHOUSE_ADDR", cfg.ClickHouse.Addr)
	cfg.ClickHouse.Database = envOrValue("ALPHAFLOW_CLICKHOUSE_DATABASE", cfg.ClickHouse.Database)
	cfg.ClickHouse.Username = envOrValue("ALPHAFLOW_CLICKHOUSE_USERNAME", cfg.ClickHouse.Username)
	cfg.ClickHouse.Password = envOrValue("ALPHAFLOW_CLICKHOUSE_PASSWORD", cfg.ClickHouse.Password)

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

func envOrValue(name string, value string) string {
	envValue := strings.TrimSpace(os.Getenv(name))
	if envValue != "" {
		return envValue
	}
	return strings.TrimSpace(value)
}

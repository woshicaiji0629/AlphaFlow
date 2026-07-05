package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"alphaflow/go-service/pkg/configutil"
)

type Config struct {
	Binance    BinanceConfig    `toml:"binance"`
	Gate       GateConfig       `toml:"gate"`
	Bitget     BitgetConfig     `toml:"bitget"`
	Bybit      BybitConfig      `toml:"bybit"`
	NATS       NATSConfig       `toml:"nats"`
	ClickHouse ClickHouseConfig `toml:"clickhouse"`
	Backfill   BackfillConfig   `toml:"backfill_queue"`
	Logging    LoggingConfig    `toml:"logging"`
}

type BinanceConfig struct {
	Enabled              bool     `toml:"enabled"`
	WebSocketConnections int      `toml:"websocket_connections"`
	Symbols              []string `toml:"symbols"`
}

type GateConfig struct {
	Enabled              bool     `toml:"enabled"`
	WebSocketConnections int      `toml:"websocket_connections"`
	Symbols              []string `toml:"symbols"`
}

type BitgetConfig struct {
	Enabled              bool     `toml:"enabled"`
	WebSocketConnections int      `toml:"websocket_connections"`
	Symbols              []string `toml:"symbols"`
}

type BybitConfig struct {
	Enabled              bool     `toml:"enabled"`
	WebSocketConnections int      `toml:"websocket_connections"`
	Symbols              []string `toml:"symbols"`
}

type NATSConfig struct {
	URL string `toml:"url"`
}

type ClickHouseConfig struct {
	Enabled              bool   `toml:"enabled"`
	Addr                 string `toml:"addr"`
	Database             string `toml:"database"`
	Username             string `toml:"username"`
	Password             string `toml:"password"`
	DialTimeout          string `toml:"dial_timeout"`
	ReadTimeout          string `toml:"read_timeout"`
	RetryInterval        string `toml:"retry_interval"`
	RetryBatch           int    `toml:"retry_batch"`
	MaxPending           int64  `toml:"max_pending"`
	PendingAckWait       string `toml:"pending_ack_wait"`
	PendingMaxDeliveries int    `toml:"pending_max_deliveries"`
}

type BackfillConfig struct {
	AckWait       string `toml:"ack_wait"`
	MaxDeliveries int    `toml:"max_deliveries"`
	MaxPending    int64  `toml:"max_pending"`
	WorkerEnabled bool   `toml:"worker_enabled"`
	WorkerBatch   int    `toml:"worker_batch"`
	WorkerMaxWait string `toml:"worker_max_wait"`
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

	if err := configutil.DecodeTOMLFileStrict(path, &cfg); err != nil {
		return Config{}, err
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
		NATS: NATSConfig{
			URL: "nats://localhost:4222",
		},
		ClickHouse: ClickHouseConfig{
			Enabled:              false,
			Addr:                 "localhost:9000",
			Database:             "alphaflow",
			Username:             "default",
			Password:             "",
			DialTimeout:          "5s",
			ReadTimeout:          "30s",
			RetryInterval:        "10s",
			RetryBatch:           100,
			MaxPending:           100000,
			PendingAckWait:       "30s",
			PendingMaxDeliveries: 5,
		},
		Backfill: BackfillConfig{
			AckWait:       "30m",
			MaxDeliveries: 3,
			MaxPending:    10000,
			WorkerEnabled: false,
			WorkerBatch:   1,
			WorkerMaxWait: "1s",
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
	validators := []func(Config) error{
		validateExchangeSymbols,
		validateClickHouse,
		validateBackfillQueue,
	}
	for _, validator := range validators {
		if err := validator(cfg); err != nil {
			return err
		}
	}
	return nil
}

func validateBackfillQueue(cfg Config) error {
	if strings.TrimSpace(cfg.NATS.URL) == "" {
		return fmt.Errorf("nats url cannot be empty")
	}
	if _, err := BackfillAckWait(cfg); err != nil {
		return err
	}
	if cfg.Backfill.MaxDeliveries <= 0 {
		return fmt.Errorf("backfill_queue.max_deliveries must be positive")
	}
	if cfg.Backfill.WorkerBatch <= 0 {
		return fmt.Errorf("backfill_queue.worker_batch must be positive")
	}
	if _, err := BackfillWorkerMaxWait(cfg); err != nil {
		return err
	}
	return nil
}

func validateExchangeSymbols(cfg Config) error {
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
	return nil
}

func validateClickHouse(cfg Config) error {
	if !cfg.ClickHouse.Enabled {
		return nil
	}
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
	if strings.TrimSpace(cfg.NATS.URL) == "" {
		return fmt.Errorf("nats url cannot be empty when clickhouse enabled")
	}
	if _, err := ClickHousePendingAckWait(cfg); err != nil {
		return err
	}
	if cfg.ClickHouse.PendingMaxDeliveries <= 0 {
		return fmt.Errorf("clickhouse pending_max_deliveries must be positive")
	}
	return nil
}

func resolvePath(configPath string) string {
	value := strings.TrimSpace(configPath)
	if value == "" {
		value = strings.TrimSpace(os.Getenv("ALPHAFLOW_MARKET_CONFIG"))
	}
	if value == "" {
		value = "configs/market-data.local.toml"
	}
	return filepath.Clean(value)
}

func normalize(cfg *Config) {
	cfg.NATS.URL = envOrValue("ALPHAFLOW_NATS_URL", cfg.NATS.URL)
	cfg.ClickHouse.Addr = envOrValue("ALPHAFLOW_CLICKHOUSE_ADDR", cfg.ClickHouse.Addr)
	cfg.ClickHouse.Database = envOrValue("ALPHAFLOW_CLICKHOUSE_DATABASE", cfg.ClickHouse.Database)
	cfg.ClickHouse.Username = envOrValue("ALPHAFLOW_CLICKHOUSE_USERNAME", cfg.ClickHouse.Username)
	cfg.ClickHouse.Password = envOrValue("ALPHAFLOW_CLICKHOUSE_PASSWORD", cfg.ClickHouse.Password)
	cfg.ClickHouse.PendingAckWait = strings.TrimSpace(cfg.ClickHouse.PendingAckWait)
	cfg.Backfill.AckWait = strings.TrimSpace(cfg.Backfill.AckWait)
	cfg.Backfill.WorkerMaxWait = strings.TrimSpace(cfg.Backfill.WorkerMaxWait)

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

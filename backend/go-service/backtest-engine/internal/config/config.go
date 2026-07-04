package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alphaflow/go-service/pkg/configutil"
)

type Config struct {
	Runtime    RuntimeConfig    `toml:"runtime"`
	Data       DataConfig       `toml:"data"`
	Sizing     SizingConfig     `toml:"sizing"`
	Fee        FeeConfig        `toml:"fee"`
	ClickHouse ClickHouseConfig `toml:"clickhouse"`
	Logging    LoggingConfig    `toml:"logging"`
}

type RuntimeConfig struct {
	RunID       string `toml:"run_id"`
	StrategySet string `toml:"strategy_set"`
}

type DataConfig struct {
	Exchange         string   `toml:"exchange"`
	Market           string   `toml:"market"`
	Symbols          []string `toml:"symbols"`
	Interval         string   `toml:"interval"`
	ConfirmIntervals []string `toml:"confirm_intervals"`
	StartTime        string   `toml:"start_time"`
	EndTime          string   `toml:"end_time"`
}

type SizingConfig struct {
	InitialEquity        float64 `toml:"initial_equity"`
	MarginQuote          float64 `toml:"margin_quote"`
	Leverage             float64 `toml:"leverage"`
	MaxPositionSize      float64 `toml:"max_position_size"`
	MinOpenConfidence    float64 `toml:"min_open_confidence"`
	DisableShortExposure bool    `toml:"disable_short_exposure"`
}

type FeeConfig struct {
	FeeRate   float64 `toml:"fee_rate"`
	RebatePct float64 `toml:"rebate_pct"`
}

type ClickHouseConfig struct {
	Enabled     bool   `toml:"enabled"`
	Addr        string `toml:"addr"`
	Database    string `toml:"database"`
	Username    string `toml:"username"`
	Password    string `toml:"password"`
	DialTimeout string `toml:"dial_timeout"`
	ReadTimeout string `toml:"read_timeout"`
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
		Runtime: RuntimeConfig{
			RunID:       "local-backtest",
			StrategySet: "supertrend",
		},
		Data: DataConfig{
			Exchange:         "binance",
			Market:           "um",
			Symbols:          []string{"ETHUSDT"},
			Interval:         "3m",
			ConfirmIntervals: []string{"5m", "10m", "15m", "30m"},
		},
		Sizing: SizingConfig{
			InitialEquity:     10000,
			MarginQuote:       100,
			Leverage:          100,
			MaxPositionSize:   1,
			MinOpenConfidence: 0.65,
		},
		Fee: FeeConfig{
			FeeRate: 0.0006,
		},
		ClickHouse: ClickHouseConfig{
			Enabled:     false,
			Addr:        "localhost:9000",
			Database:    "alphaflow",
			Username:    "default",
			Password:    "",
			DialTimeout: "5s",
			ReadTimeout: "30s",
		},
		Logging: LoggingConfig{
			Service:    "backtest-engine",
			Level:      "info",
			Format:     "json",
			Output:     "stdout",
			Dir:        "logs",
			Filename:   "backtest-engine.log",
			MaxSizeMB:  100,
			MaxBackups: 10,
			MaxAgeDays: 30,
			Compress:   true,
		},
	}
}

func ClickHouseDialTimeout(cfg Config) (time.Duration, error) {
	return parseDuration("clickhouse.dial_timeout", cfg.ClickHouse.DialTimeout)
}

func ClickHouseReadTimeout(cfg Config) (time.Duration, error) {
	return parseDuration("clickhouse.read_timeout", cfg.ClickHouse.ReadTimeout)
}

func StartTime(cfg Config) (time.Time, error) {
	return parseTime("data.start_time", cfg.Data.StartTime)
}

func EndTime(cfg Config) (time.Time, error) {
	return parseTime("data.end_time", cfg.Data.EndTime)
}

func resolvePath(configPath string) string {
	value := strings.TrimSpace(configPath)
	if value == "" {
		value = strings.TrimSpace(os.Getenv("ALPHAFLOW_BACKTEST_ENGINE_CONFIG"))
	}
	if value == "" {
		value = "backtest-engine/configs/local.toml"
	}
	return filepath.Clean(value)
}

func normalize(cfg *Config) {
	cfg.ClickHouse.Addr = envOrValue("ALPHAFLOW_CLICKHOUSE_ADDR", cfg.ClickHouse.Addr)
	cfg.ClickHouse.Database = envOrValue("ALPHAFLOW_CLICKHOUSE_DATABASE", cfg.ClickHouse.Database)
	cfg.ClickHouse.Username = envOrValue("ALPHAFLOW_CLICKHOUSE_USERNAME", cfg.ClickHouse.Username)
	cfg.ClickHouse.Password = envOrValue("ALPHAFLOW_CLICKHOUSE_PASSWORD", cfg.ClickHouse.Password)
	cfg.Runtime.RunID = strings.TrimSpace(cfg.Runtime.RunID)
	cfg.Runtime.StrategySet = strings.TrimSpace(cfg.Runtime.StrategySet)
	cfg.Data.Exchange = strings.ToLower(strings.TrimSpace(cfg.Data.Exchange))
	cfg.Data.Market = strings.ToLower(strings.TrimSpace(cfg.Data.Market))
	cfg.Data.Interval = strings.TrimSpace(cfg.Data.Interval)
	cfg.Data.StartTime = strings.TrimSpace(cfg.Data.StartTime)
	cfg.Data.EndTime = strings.TrimSpace(cfg.Data.EndTime)
	for index, symbol := range cfg.Data.Symbols {
		cfg.Data.Symbols[index] = strings.ToUpper(strings.TrimSpace(symbol))
	}
	for index, interval := range cfg.Data.ConfirmIntervals {
		cfg.Data.ConfirmIntervals[index] = strings.TrimSpace(interval)
	}
}

func validate(cfg Config) error {
	validators := []func(Config) error{
		validateRuntime,
		validateData,
		validateSizing,
		validateFee,
		validateClickHouse,
	}
	for _, validator := range validators {
		if err := validator(cfg); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntime(cfg Config) error {
	if cfg.Runtime.RunID == "" {
		return fmt.Errorf("runtime.run_id cannot be empty")
	}
	if cfg.Runtime.StrategySet == "" {
		return fmt.Errorf("runtime.strategy_set cannot be empty")
	}
	return nil
}

func validateData(cfg Config) error {
	if cfg.Data.Exchange == "" {
		return fmt.Errorf("data.exchange cannot be empty")
	}
	if cfg.Data.Market == "" {
		return fmt.Errorf("data.market cannot be empty")
	}
	if len(cfg.Data.Symbols) == 0 {
		return fmt.Errorf("data.symbols cannot be empty")
	}
	for index, symbol := range cfg.Data.Symbols {
		if symbol == "" {
			return fmt.Errorf("data.symbols[%d] cannot be empty", index)
		}
	}
	if cfg.Data.Interval == "" {
		return fmt.Errorf("data.interval cannot be empty")
	}
	if cfg.Data.StartTime == "" {
		return fmt.Errorf("data.start_time cannot be empty")
	}
	if cfg.Data.EndTime == "" {
		return fmt.Errorf("data.end_time cannot be empty")
	}
	startTime, err := StartTime(cfg)
	if err != nil {
		return err
	}
	endTime, err := EndTime(cfg)
	if err != nil {
		return err
	}
	if !endTime.After(startTime) {
		return fmt.Errorf("data.end_time must be after data.start_time")
	}
	return nil
}

func validateSizing(cfg Config) error {
	if cfg.Sizing.InitialEquity <= 0 {
		return fmt.Errorf("sizing.initial_equity must be positive")
	}
	if cfg.Sizing.MarginQuote < 0 {
		return fmt.Errorf("sizing.margin_quote cannot be negative")
	}
	if cfg.Sizing.Leverage < 0 {
		return fmt.Errorf("sizing.leverage cannot be negative")
	}
	return nil
}

func validateFee(cfg Config) error {
	if cfg.Fee.FeeRate < 0 {
		return fmt.Errorf("fee.fee_rate cannot be negative")
	}
	if cfg.Fee.RebatePct < 0 || cfg.Fee.RebatePct > 100 {
		return fmt.Errorf("fee.rebate_pct must be between 0 and 100")
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
	return nil
}

func parseDuration(name string, value string) (time.Duration, error) {
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return parsed, nil
}

func parseTime(name string, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}

func envOrValue(name string, value string) string {
	envValue := strings.TrimSpace(os.Getenv(name))
	if envValue != "" {
		return envValue
	}
	return strings.TrimSpace(value)
}

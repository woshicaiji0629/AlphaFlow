package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alphaflow/go-service/pkg/configutil"
	"alphaflow/go-service/pkg/redisclient"
	"alphaflow/go-service/pkg/strategy"
)

type Config struct {
	Runtime    RuntimeConfig    `toml:"runtime"`
	Redis      RedisConfig      `toml:"redis"`
	NATS       NATSConfig       `toml:"nats"`
	Output     OutputConfig     `toml:"output"`
	Position   PositionConfig   `toml:"position"`
	Strategies StrategiesConfig `toml:"strategies"`
	Sizing     SizingConfig     `toml:"sizing"`
	Fee        FeeConfig        `toml:"fee"`
	ClickHouse ClickHouseConfig `toml:"clickhouse"`
	Targets    []TargetConfig   `toml:"targets"`
	Logging    LoggingConfig    `toml:"logging"`
}

type RuntimeConfig struct {
	ScanInterval string `toml:"scan_interval"`
}

type RedisConfig struct {
	Addr         string `toml:"addr"`
	Password     string `toml:"password"`
	DB           int    `toml:"db"`
	PoolSize     int    `toml:"pool_size"`
	MinIdleConns int    `toml:"min_idle_conns"`
}

type NATSConfig struct {
	URL string `toml:"url"`
}

type OutputConfig struct {
	Mode       string `toml:"mode"`
	Stream     string `toml:"stream"`
	Subject    string `toml:"subject"`
	DefaultTTL string `toml:"default_ttl"`
}

type PositionConfig struct {
	Scope   string `toml:"scope"`
	Account string `toml:"account"`
}

type StrategiesConfig struct {
	Enabled []string `toml:"enabled"`
}

type SizingConfig struct {
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

type TargetConfig struct {
	Exchange         string   `toml:"exchange"`
	Market           string   `toml:"market"`
	Symbol           string   `toml:"symbol"`
	Interval         string   `toml:"interval"`
	ConfirmIntervals []string `toml:"confirm_intervals"`
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
			ScanInterval: "5s",
		},
		Redis: RedisConfig{
			Addr:         "localhost:6380",
			PoolSize:     20,
			MinIdleConns: 5,
		},
		NATS: NATSConfig{
			URL: "nats://localhost:4222",
		},
		Output: OutputConfig{
			Mode:       "bus",
			Stream:     "ALPHAFLOW_STRATEGY",
			Subject:    "strategy.decision",
			DefaultTTL: "30s",
		},
		Position: PositionConfig{
			Scope:   string(strategy.PositionScopePaper),
			Account: "default",
		},
		Strategies: StrategiesConfig{
			Enabled: []string{"supertrend"},
		},
		Sizing: SizingConfig{
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
		Targets: []TargetConfig{{
			Exchange:         "binance",
			Market:           "um",
			Symbol:           "ETHUSDT",
			Interval:         "3m",
			ConfirmIntervals: []string{"5m", "10m", "15m", "30m"},
		}},
		Logging: LoggingConfig{
			Service:    "strategy-engine",
			Level:      "info",
			Format:     "json",
			Output:     "stdout",
			Dir:        "logs",
			Filename:   "strategy-engine.log",
			MaxSizeMB:  100,
			MaxBackups: 10,
			MaxAgeDays: 30,
			Compress:   true,
		},
	}
}

func ScanInterval(cfg Config) (time.Duration, error) {
	return parseDuration("runtime.scan_interval", cfg.Runtime.ScanInterval)
}

func ClickHouseDialTimeout(cfg Config) (time.Duration, error) {
	return parseDuration("clickhouse.dial_timeout", cfg.ClickHouse.DialTimeout)
}

func ClickHouseReadTimeout(cfg Config) (time.Duration, error) {
	return parseDuration("clickhouse.read_timeout", cfg.ClickHouse.ReadTimeout)
}

func OutputDefaultTTL(cfg Config) (time.Duration, error) {
	return parseDuration("output.default_ttl", cfg.Output.DefaultTTL)
}

func RedisClientConfig(cfg Config) redisclient.Config {
	return redisclient.Config{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
	}
}

func PositionScope(cfg Config) strategy.PositionScope {
	return strategy.PositionScope(cfg.Position.Scope)
}

func Targets(cfg Config) []strategy.Target {
	scope := PositionScope(cfg)
	targets := make([]strategy.Target, 0, len(cfg.Targets))
	for _, item := range cfg.Targets {
		targets = append(targets, strategy.Target{
			Exchange: item.Exchange,
			Market:   item.Market,
			Symbol:   item.Symbol,
			Interval: item.Interval,
			Account:  cfg.Position.Account,
			Scope:    scope,
		})
	}
	return targets
}

func ConfirmIntervals(item TargetConfig) []string {
	intervals := make([]string, 0, len(item.ConfirmIntervals))
	for _, interval := range item.ConfirmIntervals {
		interval = strings.TrimSpace(interval)
		if interval != "" {
			intervals = append(intervals, interval)
		}
	}
	return intervals
}

func resolvePath(configPath string) string {
	value := strings.TrimSpace(configPath)
	if value == "" {
		value = strings.TrimSpace(os.Getenv("ALPHAFLOW_STRATEGY_ENGINE_CONFIG"))
	}
	if value == "" {
		value = "configs/strategy-engine.local.toml"
	}
	return filepath.Clean(value)
}

func normalize(cfg *Config) {
	cfg.Redis.Addr = envOrValue("ALPHAFLOW_REDIS_ADDR", cfg.Redis.Addr)
	cfg.Redis.Password = envOrValue("ALPHAFLOW_REDIS_PASSWORD", cfg.Redis.Password)
	cfg.NATS.URL = envOrValue("ALPHAFLOW_NATS_URL", cfg.NATS.URL)
	cfg.ClickHouse.Addr = envOrValue("ALPHAFLOW_CLICKHOUSE_ADDR", cfg.ClickHouse.Addr)
	cfg.ClickHouse.Database = envOrValue("ALPHAFLOW_CLICKHOUSE_DATABASE", cfg.ClickHouse.Database)
	cfg.ClickHouse.Username = envOrValue("ALPHAFLOW_CLICKHOUSE_USERNAME", cfg.ClickHouse.Username)
	cfg.ClickHouse.Password = envOrValue("ALPHAFLOW_CLICKHOUSE_PASSWORD", cfg.ClickHouse.Password)
	for index, item := range cfg.Targets {
		cfg.Targets[index].Exchange = strings.ToLower(strings.TrimSpace(item.Exchange))
		cfg.Targets[index].Market = strings.ToLower(strings.TrimSpace(item.Market))
		cfg.Targets[index].Symbol = strings.ToUpper(strings.TrimSpace(item.Symbol))
		cfg.Targets[index].Interval = strings.TrimSpace(item.Interval)
		for intervalIndex, interval := range item.ConfirmIntervals {
			cfg.Targets[index].ConfirmIntervals[intervalIndex] = strings.TrimSpace(interval)
		}
	}
	for index, name := range cfg.Strategies.Enabled {
		cfg.Strategies.Enabled[index] = strings.ToLower(strings.TrimSpace(name))
	}
	cfg.Position.Scope = strings.TrimSpace(cfg.Position.Scope)
	cfg.Position.Account = strings.TrimSpace(cfg.Position.Account)
	cfg.Output.Mode = strings.ToLower(strings.TrimSpace(cfg.Output.Mode))
	cfg.Output.Stream = strings.TrimSpace(cfg.Output.Stream)
	cfg.Output.Subject = strings.TrimSpace(cfg.Output.Subject)
}

func validate(cfg Config) error {
	validators := []func(Config) error{
		validateRuntime,
		validateRedis,
		validateOutput,
		validatePosition,
		validateStrategies,
		validateTargets,
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
	if _, err := ScanInterval(cfg); err != nil {
		return err
	}
	return nil
}

func validateRedis(cfg Config) error {
	if strings.TrimSpace(cfg.Redis.Addr) == "" {
		return fmt.Errorf("redis addr cannot be empty")
	}
	return nil
}

func validateOutput(cfg Config) error {
	switch cfg.Output.Mode {
	case "bus", "local":
	default:
		return fmt.Errorf("unsupported output mode %q", cfg.Output.Mode)
	}
	if cfg.Output.Mode == "bus" && strings.TrimSpace(cfg.Output.Stream) == "" {
		return fmt.Errorf("output.stream cannot be empty when output.mode is bus")
	}
	if cfg.Output.Mode == "bus" && strings.TrimSpace(cfg.Output.Subject) == "" {
		return fmt.Errorf("output.subject cannot be empty when output.mode is bus")
	}
	if _, err := OutputDefaultTTL(cfg); err != nil {
		return err
	}
	return nil
}

func validatePosition(cfg Config) error {
	switch PositionScope(cfg) {
	case strategy.PositionScopePaper:
	default:
		return fmt.Errorf("unsupported online position scope %q", cfg.Position.Scope)
	}
	return nil
}

func validateStrategies(cfg Config) error {
	if len(cfg.Strategies.Enabled) == 0 {
		return fmt.Errorf("strategies.enabled cannot be empty")
	}
	for index, name := range cfg.Strategies.Enabled {
		if name == "" {
			return fmt.Errorf("strategies.enabled[%d] cannot be empty", index)
		}
	}
	return nil
}

func validateTargets(cfg Config) error {
	if len(cfg.Targets) == 0 {
		return fmt.Errorf("targets cannot be empty")
	}
	for index, target := range cfg.Targets {
		if target.Exchange == "" {
			return fmt.Errorf("targets[%d].exchange cannot be empty", index)
		}
		if target.Market == "" {
			return fmt.Errorf("targets[%d].market cannot be empty", index)
		}
		if target.Symbol == "" {
			return fmt.Errorf("targets[%d].symbol cannot be empty", index)
		}
		if target.Interval == "" {
			return fmt.Errorf("targets[%d].interval cannot be empty", index)
		}
	}
	return nil
}

func validateSizing(cfg Config) error {
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

func envOrValue(name string, value string) string {
	envValue := strings.TrimSpace(os.Getenv(name))
	if envValue != "" {
		return envValue
	}
	return strings.TrimSpace(value)
}

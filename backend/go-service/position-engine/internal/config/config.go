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
	"alphaflow/go-service/pkg/strategybus"
	"alphaflow/go-service/pkg/strategyroute"
)

type Config struct {
	Runtime     RuntimeConfig     `toml:"runtime"`
	Redis       RedisConfig       `toml:"redis"`
	Input       InputConfig       `toml:"input"`
	Idempotency IdempotencyConfig `toml:"idempotency"`
	Position    PositionConfig    `toml:"position"`
	Sizing      SizingConfig      `toml:"sizing"`
	Fee         FeeConfig         `toml:"fee"`
	Routes      []RouteConfig     `toml:"routes"`
	Logging     LoggingConfig     `toml:"logging"`
}

type RuntimeConfig struct {
	Service string `toml:"service"`
}

type RedisConfig struct {
	Addr         string `toml:"addr"`
	Password     string `toml:"password"`
	DB           int    `toml:"db"`
	PoolSize     int    `toml:"pool_size"`
	MinIdleConns int    `toml:"min_idle_conns"`
}

type InputConfig struct {
	Stream           string `toml:"stream"`
	Group            string `toml:"group"`
	Consumer         string `toml:"consumer"`
	Block            string `toml:"block"`
	Batch            int64  `toml:"batch"`
	DefaultTTL       string `toml:"default_ttl"`
	PendingIdle      string `toml:"pending_idle"`
	DeadLetterStream string `toml:"dead_letter_stream"`
	MaxDeliveries    int64  `toml:"max_deliveries"`
}

type IdempotencyConfig struct {
	Prefix        string `toml:"prefix"`
	ProcessingTTL string `toml:"processing_ttl"`
	CompletedTTL  string `toml:"completed_ttl"`
}

type PositionConfig struct {
	Scope   string `toml:"scope"`
	Account string `toml:"account"`
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

type RouteConfig struct {
	Strategy string `toml:"strategy"`
	Sink     string `toml:"sink"`
	Account  string `toml:"account"`
	RunID    string `toml:"run_id"`
	Notifier string `toml:"notifier"`
	Enabled  bool   `toml:"enabled"`
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

func Routes(cfg Config) ([]strategyroute.Route, error) {
	routes := make([]strategyroute.Route, 0, len(cfg.Routes))
	for _, item := range cfg.Routes {
		sink, err := strategyroute.ParseSink(item.Sink)
		if err != nil {
			return nil, err
		}
		routes = append(routes, strategyroute.Route{
			StrategyName: item.Strategy,
			Sink:         sink,
			Account:      item.Account,
			RunID:        item.RunID,
			Notifier:     item.Notifier,
			Enabled:      item.Enabled,
		})
	}
	return routes, nil
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

func RedisBusOptions(cfg Config) (strategybus.RedisOptions, error) {
	block, err := InputBlock(cfg)
	if err != nil {
		return strategybus.RedisOptions{}, err
	}
	pendingIdle, err := InputPendingIdle(cfg)
	if err != nil {
		return strategybus.RedisOptions{}, err
	}
	return strategybus.RedisOptions{
		Stream:           cfg.Input.Stream,
		Group:            cfg.Input.Group,
		Consumer:         cfg.Input.Consumer,
		Block:            block,
		Batch:            cfg.Input.Batch,
		PendingIdle:      pendingIdle,
		DeadLetterStream: cfg.Input.DeadLetterStream,
		MaxDeliveries:    cfg.Input.MaxDeliveries,
	}, nil
}

func PositionScope(cfg Config) strategy.PositionScope {
	return strategy.PositionScope(cfg.Position.Scope)
}

func InputDefaultTTL(cfg Config) (time.Duration, error) {
	return parseDuration("input.default_ttl", cfg.Input.DefaultTTL)
}

func InputPendingIdle(cfg Config) (time.Duration, error) {
	return parseDuration("input.pending_idle", cfg.Input.PendingIdle)
}

func InputBlock(cfg Config) (time.Duration, error) {
	return parseDuration("input.block", cfg.Input.Block)
}

func IdempotencyProcessingTTL(cfg Config) (time.Duration, error) {
	return parseDuration("idempotency.processing_ttl", cfg.Idempotency.ProcessingTTL)
}

func IdempotencyCompletedTTL(cfg Config) (time.Duration, error) {
	return parseDuration("idempotency.completed_ttl", cfg.Idempotency.CompletedTTL)
}

func defaultConfig() Config {
	return Config{
		Runtime: RuntimeConfig{
			Service: "position-engine",
		},
		Redis: RedisConfig{
			Addr:         "localhost:6380",
			PoolSize:     20,
			MinIdleConns: 5,
		},
		Input: InputConfig{
			Stream:           strategybus.DefaultDecisionStream,
			Group:            "position-engine",
			Consumer:         "local",
			Block:            "5s",
			Batch:            10,
			DefaultTTL:       "60s",
			PendingIdle:      "30s",
			DeadLetterStream: strategybus.DefaultDecisionStream + ":dead",
			MaxDeliveries:    5,
		},
		Idempotency: IdempotencyConfig{
			Prefix:        "position:decision:idempotency",
			ProcessingTTL: "10m",
			CompletedTTL:  "24h",
		},
		Position: PositionConfig{
			Scope:   string(strategy.PositionScopePaper),
			Account: "paper-default",
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
		Logging: LoggingConfig{
			Service:    "position-engine",
			Level:      "info",
			Format:     "json",
			Output:     "stdout",
			Dir:        "logs",
			Filename:   "position-engine.log",
			MaxSizeMB:  100,
			MaxBackups: 10,
			MaxAgeDays: 30,
			Compress:   true,
		},
	}
}

func resolvePath(configPath string) string {
	value := strings.TrimSpace(configPath)
	if value == "" {
		value = strings.TrimSpace(os.Getenv("ALPHAFLOW_POSITION_ENGINE_CONFIG"))
	}
	if value == "" {
		value = "position-engine/configs/local.toml"
	}
	return filepath.Clean(value)
}

func normalize(cfg *Config) {
	cfg.Runtime.Service = strings.TrimSpace(cfg.Runtime.Service)
	cfg.Redis.Addr = envOrValue("ALPHAFLOW_REDIS_ADDR", cfg.Redis.Addr)
	cfg.Redis.Password = envOrValue("ALPHAFLOW_REDIS_PASSWORD", cfg.Redis.Password)
	cfg.Input.Stream = strings.TrimSpace(cfg.Input.Stream)
	cfg.Input.Group = strings.TrimSpace(cfg.Input.Group)
	cfg.Input.Consumer = strings.TrimSpace(cfg.Input.Consumer)
	cfg.Input.Block = strings.TrimSpace(cfg.Input.Block)
	cfg.Input.DefaultTTL = strings.TrimSpace(cfg.Input.DefaultTTL)
	cfg.Input.PendingIdle = strings.TrimSpace(cfg.Input.PendingIdle)
	cfg.Input.DeadLetterStream = strings.TrimSpace(cfg.Input.DeadLetterStream)
	cfg.Idempotency.Prefix = strings.TrimSpace(cfg.Idempotency.Prefix)
	cfg.Idempotency.ProcessingTTL = strings.TrimSpace(cfg.Idempotency.ProcessingTTL)
	cfg.Idempotency.CompletedTTL = strings.TrimSpace(cfg.Idempotency.CompletedTTL)
	cfg.Position.Scope = strings.TrimSpace(cfg.Position.Scope)
	cfg.Position.Account = strings.TrimSpace(cfg.Position.Account)
	for index, route := range cfg.Routes {
		cfg.Routes[index].Strategy = strings.TrimSpace(route.Strategy)
		cfg.Routes[index].Sink = strings.ToLower(strings.TrimSpace(route.Sink))
		cfg.Routes[index].Account = strings.TrimSpace(route.Account)
		cfg.Routes[index].RunID = strings.TrimSpace(route.RunID)
		cfg.Routes[index].Notifier = strings.TrimSpace(route.Notifier)
	}
}

func validate(cfg Config) error {
	if cfg.Runtime.Service == "" {
		return fmt.Errorf("runtime.service cannot be empty")
	}
	if strings.TrimSpace(cfg.Redis.Addr) == "" {
		return fmt.Errorf("redis addr cannot be empty")
	}
	if cfg.Input.Stream == "" {
		return fmt.Errorf("input.stream cannot be empty")
	}
	if cfg.Input.Group == "" {
		return fmt.Errorf("input.group cannot be empty")
	}
	if cfg.Input.Consumer == "" {
		return fmt.Errorf("input.consumer cannot be empty")
	}
	if _, err := InputBlock(cfg); err != nil {
		return err
	}
	if _, err := InputDefaultTTL(cfg); err != nil {
		return err
	}
	if _, err := InputPendingIdle(cfg); err != nil {
		return err
	}
	if cfg.Input.Batch <= 0 {
		return fmt.Errorf("input.batch must be positive")
	}
	if cfg.Input.DeadLetterStream == "" {
		return fmt.Errorf("input.dead_letter_stream cannot be empty")
	}
	if cfg.Input.MaxDeliveries <= 0 {
		return fmt.Errorf("input.max_deliveries must be positive")
	}
	if cfg.Idempotency.Prefix == "" {
		return fmt.Errorf("idempotency.prefix cannot be empty")
	}
	if _, err := IdempotencyProcessingTTL(cfg); err != nil {
		return err
	}
	if _, err := IdempotencyCompletedTTL(cfg); err != nil {
		return err
	}
	if cfg.Position.Scope == "" {
		return fmt.Errorf("position.scope cannot be empty")
	}
	if PositionScope(cfg) != strategy.PositionScopePaper {
		return fmt.Errorf("unsupported position scope %q", cfg.Position.Scope)
	}
	if cfg.Position.Account == "" {
		return fmt.Errorf("position.account cannot be empty")
	}
	if cfg.Sizing.MarginQuote <= 0 {
		return fmt.Errorf("sizing.margin_quote must be positive")
	}
	if cfg.Sizing.Leverage <= 0 {
		return fmt.Errorf("sizing.leverage must be positive")
	}
	if cfg.Sizing.MaxPositionSize <= 0 {
		return fmt.Errorf("sizing.max_position_size must be positive")
	}
	if cfg.Sizing.MinOpenConfidence < 0 || cfg.Sizing.MinOpenConfidence > 1 {
		return fmt.Errorf("sizing.min_open_confidence must be between 0 and 1")
	}
	if cfg.Fee.FeeRate < 0 {
		return fmt.Errorf("fee.fee_rate cannot be negative")
	}
	if cfg.Fee.RebatePct < 0 || cfg.Fee.RebatePct > 100 {
		return fmt.Errorf("fee.rebate_pct must be between 0 and 100")
	}
	if len(cfg.Routes) == 0 {
		return fmt.Errorf("routes cannot be empty")
	}
	for index, route := range cfg.Routes {
		if route.Strategy == "" {
			return fmt.Errorf("routes[%d].strategy cannot be empty", index)
		}
		if _, err := strategyroute.ParseSink(route.Sink); err != nil {
			return fmt.Errorf("routes[%d].sink: %w", index, err)
		}
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

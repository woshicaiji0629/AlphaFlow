package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alphaflow/go-service/pkg/configutil"
	"alphaflow/go-service/pkg/strategyspec"
)

type Config struct {
	Runtime     RuntimeConfig      `toml:"runtime"`
	Strategy    strategyspec.Spec  `toml:"strategy"`
	Data        DataConfig         `toml:"data"`
	Sizing      SizingConfig       `toml:"sizing"`
	Fee         FeeConfig          `toml:"fee"`
	Execution   ExecutionConfig    `toml:"execution"`
	Result      ResultConfig       `toml:"result"`
	SymbolSpecs []SymbolSpecConfig `toml:"symbol_specs"`
	ClickHouse  ClickHouseConfig   `toml:"clickhouse"`
	Logging     LoggingConfig      `toml:"logging"`
}

type RuntimeConfig struct {
	RunID       string `toml:"run_id"`
	StrategySet string `toml:"strategy_set"`
}

type DataConfig struct {
	Exchange             string   `toml:"exchange"`
	Market               string   `toml:"market"`
	Symbols              []string `toml:"symbols"`
	Interval             string   `toml:"interval"`
	ConfirmIntervals     []string `toml:"confirm_intervals"`
	WarmupBars           int64    `toml:"warmup_bars"`
	IndicatorBatchSize   int      `toml:"indicator_batch_size"`
	IndicatorConcurrency int      `toml:"indicator_concurrency"`
	StartTime            string   `toml:"start_time"`
	EndTime              string   `toml:"end_time"`
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

type ExecutionConfig struct {
	SlippageBps float64 `toml:"slippage_bps"`
}

type ResultConfig struct {
	EventBatchSize int    `toml:"event_batch_size"`
	TradeBatchSize int    `toml:"trade_batch_size"`
	ReportJSONPath string `toml:"report_json_path"`
}

type SymbolSpecConfig struct {
	Exchange     string  `toml:"exchange"`
	Market       string  `toml:"market"`
	Symbol       string  `toml:"symbol"`
	PriceTick    float64 `toml:"price_tick"`
	QuantityStep float64 `toml:"quantity_step"`
	MinQuantity  float64 `toml:"min_quantity"`
	MinNotional  float64 `toml:"min_notional"`
	ContractSize float64 `toml:"contract_size"`
	QuantityUnit string  `toml:"quantity_unit"`
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
			Exchange:           "binance",
			Market:             "um",
			Symbols:            []string{"ETHUSDT"},
			Interval:           "3m",
			ConfirmIntervals:   []string{"5m", "10m", "15m", "30m"},
			WarmupBars:         300,
			IndicatorBatchSize: 30,
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
		Result: ResultConfig{
			EventBatchSize: 1000,
			TradeBatchSize: 1000,
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
		value = "configs/backtest-engine.local.toml"
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
	cfg.Strategy = strategyspec.Normalize(cfg.Strategy)
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
	for index := range cfg.SymbolSpecs {
		spec := &cfg.SymbolSpecs[index]
		spec.Exchange = strings.ToLower(strings.TrimSpace(spec.Exchange))
		if spec.Exchange == "" {
			spec.Exchange = cfg.Data.Exchange
		}
		spec.Market = strings.ToLower(strings.TrimSpace(spec.Market))
		if spec.Market == "" {
			spec.Market = cfg.Data.Market
		}
		spec.Symbol = strings.ToUpper(strings.TrimSpace(spec.Symbol))
		spec.QuantityUnit = strings.ToLower(strings.TrimSpace(spec.QuantityUnit))
		if spec.QuantityUnit == "" {
			spec.QuantityUnit = "base"
		}
		if spec.ContractSize <= 0 {
			spec.ContractSize = 1
		}
	}
	cfg.Result.ReportJSONPath = strings.TrimSpace(cfg.Result.ReportJSONPath)
}

func validate(cfg Config) error {
	validators := []func(Config) error{
		validateRuntime,
		validateData,
		validateSizing,
		validateFee,
		validateExecution,
		validateResult,
		validateSymbolSpecs,
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
	if cfg.Strategy.Name == "" && cfg.Runtime.StrategySet == "" {
		return fmt.Errorf("strategy.name or runtime.strategy_set must be configured")
	}
	if cfg.Strategy.Name != "" && !cfg.Strategy.Enabled {
		return fmt.Errorf("strategy must be enabled")
	}
	return nil
}

func StrategySpec(cfg Config) strategyspec.Spec {
	if cfg.Strategy.Name != "" {
		return cfg.Strategy
	}
	return strategyspec.Legacy(cfg.Runtime.StrategySet)
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
	if cfg.Data.WarmupBars < 0 {
		return fmt.Errorf("data.warmup_bars cannot be negative")
	}
	if cfg.Data.IndicatorBatchSize <= 0 {
		return fmt.Errorf("data.indicator_batch_size must be positive")
	}
	if cfg.Data.IndicatorConcurrency < 0 {
		return fmt.Errorf("data.indicator_concurrency cannot be negative")
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

func validateExecution(cfg Config) error {
	if cfg.Execution.SlippageBps < 0 {
		return fmt.Errorf("execution.slippage_bps cannot be negative")
	}
	return nil
}

func validateResult(cfg Config) error {
	if cfg.Result.EventBatchSize <= 0 {
		return fmt.Errorf("result.event_batch_size must be positive")
	}
	if cfg.Result.TradeBatchSize <= 0 {
		return fmt.Errorf("result.trade_batch_size must be positive")
	}
	return nil
}

func validateSymbolSpecs(cfg Config) error {
	for index, spec := range cfg.SymbolSpecs {
		if spec.Exchange == "" {
			return fmt.Errorf("symbol_specs[%d].exchange cannot be empty", index)
		}
		if spec.Market == "" {
			return fmt.Errorf("symbol_specs[%d].market cannot be empty", index)
		}
		if spec.Symbol == "" {
			return fmt.Errorf("symbol_specs[%d].symbol cannot be empty", index)
		}
		if spec.PriceTick < 0 {
			return fmt.Errorf("symbol_specs[%d].price_tick cannot be negative", index)
		}
		if spec.QuantityStep < 0 {
			return fmt.Errorf("symbol_specs[%d].quantity_step cannot be negative", index)
		}
		if spec.MinQuantity < 0 {
			return fmt.Errorf("symbol_specs[%d].min_quantity cannot be negative", index)
		}
		if spec.MinNotional < 0 {
			return fmt.Errorf("symbol_specs[%d].min_notional cannot be negative", index)
		}
		if spec.ContractSize <= 0 {
			return fmt.Errorf("symbol_specs[%d].contract_size must be positive", index)
		}
		switch spec.QuantityUnit {
		case "base", "contract":
		default:
			return fmt.Errorf("symbol_specs[%d].quantity_unit must be base or contract", index)
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

package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"alphaflow/go-service/pkg/configutil"
	"alphaflow/go-service/pkg/executionbus"
)

type Config struct {
	NATS      NATS      `toml:"nats"`
	Redis     Redis     `toml:"redis"`
	Execution Execution `toml:"execution"`
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

func Load(path string) (Config, error) {
	cfg := Config{NATS: NATS{URL: "nats://localhost:4222", Stream: executionbus.DefaultStream, IntentSubject: executionbus.DefaultIntentSubject, ReportSubject: executionbus.DefaultReportSubject, Durable: "execution-engine", Block: "1s", AckWait: "30s", Batch: 10}, Redis: Redis{Addr: "localhost:6380", PoolSize: 20, MinIdleConns: 5}, Execution: Execution{Mode: "paper"}}
	if err := configutil.DecodeTOMLFileStrict(path, &cfg); err != nil {
		return Config{}, err
	}
	cfg.NATS.URL = envOrValue("ALPHAFLOW_NATS_URL", cfg.NATS.URL)
	cfg.Redis.Addr = envOrValue("ALPHAFLOW_REDIS_ADDR", cfg.Redis.Addr)
	cfg.Redis.Password = envOrValue("ALPHAFLOW_REDIS_PASSWORD", cfg.Redis.Password)
	if cfg.Execution.Mode != "paper" {
		return Config{}, fmt.Errorf("execution mode %q is not enabled", cfg.Execution.Mode)
	}
	return cfg, nil
}
func Block(cfg Config) (time.Duration, error)   { return time.ParseDuration(cfg.NATS.Block) }
func AckWait(cfg Config) (time.Duration, error) { return time.ParseDuration(cfg.NATS.AckWait) }
func envOrValue(name, value string) string {
	if candidate := strings.TrimSpace(os.Getenv(name)); candidate != "" {
		return candidate
	}
	return strings.TrimSpace(value)
}

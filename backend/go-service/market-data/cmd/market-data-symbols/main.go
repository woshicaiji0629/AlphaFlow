package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"alphaflow/go-service/pkg/httpclient"
	"alphaflow/go-service/pkg/logger"
)

type rankedSymbol struct {
	Symbol      string
	QuoteVolume float64
}

type exchangeSymbols struct {
	Name    string
	Symbols []string
}

func main() {
	setupLogger()

	limit := flag.Int("limit", 500, "symbols per exchange")
	output := flag.String("output", "configs/market-data.live-top500.toml", "output config path")
	timeout := flag.Duration("timeout", 30*time.Second, "fetch timeout")
	binanceBase := flag.String("binance-base", "https://fapi.binance.com", "Binance USDT-M base URL")
	gateBase := flag.String("gate-base", "https://api.gateio.ws/api/v4", "Gate API base URL")
	bitgetBase := flag.String("bitget-base", "https://api.bitget.com", "Bitget API base URL")
	bybitBase := flag.String("bybit-base", "https://api.bybit.com", "Bybit API base URL")
	flag.Parse()

	if *limit <= 0 {
		exitWithError("limit must be positive")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	client := httpclient.New()
	results := []exchangeSymbols{}

	binanceSymbols, err := fetchBinance(ctx, client, *binanceBase, *limit)
	if err != nil {
		exitWithError("fetch binance symbols failed", "error", err)
	}
	results = append(results, exchangeSymbols{Name: "binance", Symbols: binanceSymbols})

	gateSymbols, err := fetchGate(ctx, client, *gateBase, *limit)
	if err != nil {
		exitWithError("fetch gate symbols failed", "error", err)
	}
	results = append(results, exchangeSymbols{Name: "gate", Symbols: gateSymbols})

	bitgetSymbols, err := fetchBitget(ctx, client, *bitgetBase, *limit)
	if err != nil {
		exitWithError("fetch bitget symbols failed", "error", err)
	}
	results = append(results, exchangeSymbols{Name: "bitget", Symbols: bitgetSymbols})

	bybitSymbols, err := fetchBybit(ctx, client, *bybitBase, *limit)
	if err != nil {
		exitWithError("fetch bybit symbols failed", "error", err)
	}
	results = append(results, exchangeSymbols{Name: "bybit", Symbols: bybitSymbols})

	if err := os.WriteFile(*output, []byte(renderConfig(results)), 0o644); err != nil {
		exitWithError("write config failed", "path", *output, "error", err)
	}

	for _, result := range results {
		fmt.Printf("%s symbols=%d first=%s\n", result.Name, len(result.Symbols), firstSymbol(result.Symbols))
	}
	fmt.Printf("wrote %s\n", *output)
}

func setupLogger() {
	if err := logger.Setup(logger.Config{
		Service: "market-data-symbols",
		Level:   "info",
		Format:  "text",
		Output:  "stderr",
	}); err != nil {
		fmt.Fprintf(os.Stderr, "setup logger: %v\n", err)
		os.Exit(1)
	}
}

func exitWithError(message string, attrs ...any) {
	slog.Error(message, attrs...)
	os.Exit(1)
}

func fetchBinance(ctx context.Context, client *httpclient.Client, baseURL string, limit int) ([]string, error) {
	body, err := client.Get(ctx, strings.TrimRight(baseURL, "/")+"/fapi/v1/ticker/24hr", nil)
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	ranked := make([]rankedSymbol, 0, len(raw))
	for _, item := range raw {
		symbol := stringField(item, "symbol")
		if !validLinearSymbol(symbol) {
			continue
		}
		quoteVolume := numberField(item, "quoteVolume")
		ranked = append(ranked, rankedSymbol{Symbol: symbol, QuoteVolume: quoteVolume})
	}
	return topSymbols(ranked, limit), nil
}

func fetchGate(ctx context.Context, client *httpclient.Client, baseURL string, limit int) ([]string, error) {
	body, err := client.Get(ctx, strings.TrimRight(baseURL, "/")+"/futures/usdt/tickers", nil)
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	ranked := make([]rankedSymbol, 0, len(raw))
	for _, item := range raw {
		symbol := stringField(item, "contract")
		if !validGateSymbol(symbol) {
			continue
		}
		quoteVolume := firstNumberField(item, "volume_24h_quote", "volume_24h_settle", "volume_24h_base")
		ranked = append(ranked, rankedSymbol{Symbol: symbol, QuoteVolume: quoteVolume})
	}
	return topSymbols(ranked, limit), nil
}

func fetchBitget(ctx context.Context, client *httpclient.Client, baseURL string, limit int) ([]string, error) {
	body, err := client.Get(ctx, strings.TrimRight(baseURL, "/")+"/api/v2/mix/market/tickers", map[string]string{
		"productType": "USDT-FUTURES",
	})
	if err != nil {
		return nil, err
	}
	var response struct {
		Code string           `json:"code"`
		Msg  string           `json:"msg"`
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if response.Code != "00000" {
		return nil, fmt.Errorf("response code %s msg %s", response.Code, response.Msg)
	}
	ranked := make([]rankedSymbol, 0, len(response.Data))
	for _, item := range response.Data {
		symbol := stringField(item, "symbol")
		if !validLinearSymbol(symbol) {
			continue
		}
		quoteVolume := firstNumberField(item, "quoteVolume", "usdtVolume", "baseVolume")
		ranked = append(ranked, rankedSymbol{Symbol: symbol, QuoteVolume: quoteVolume})
	}
	return topSymbols(ranked, limit), nil
}

func fetchBybit(ctx context.Context, client *httpclient.Client, baseURL string, limit int) ([]string, error) {
	body, err := client.Get(ctx, strings.TrimRight(baseURL, "/")+"/v5/market/tickers", map[string]string{
		"category": "linear",
	})
	if err != nil {
		return nil, err
	}
	var response struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []map[string]any `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if response.RetCode != 0 {
		return nil, fmt.Errorf("response code %d msg %s", response.RetCode, response.RetMsg)
	}
	ranked := make([]rankedSymbol, 0, len(response.Result.List))
	for _, item := range response.Result.List {
		symbol := stringField(item, "symbol")
		if !validLinearSymbol(symbol) {
			continue
		}
		quoteVolume := firstNumberField(item, "turnover24h", "volume24h")
		ranked = append(ranked, rankedSymbol{Symbol: symbol, QuoteVolume: quoteVolume})
	}
	return topSymbols(ranked, limit), nil
}

func topSymbols(symbols []rankedSymbol, limit int) []string {
	sort.Slice(symbols, func(i int, j int) bool {
		if symbols[i].QuoteVolume == symbols[j].QuoteVolume {
			return symbols[i].Symbol < symbols[j].Symbol
		}
		return symbols[i].QuoteVolume > symbols[j].QuoteVolume
	})
	if len(symbols) > limit {
		symbols = symbols[:limit]
	}
	result := make([]string, 0, len(symbols))
	seen := map[string]struct{}{}
	for _, item := range symbols {
		if item.Symbol == "" {
			continue
		}
		if _, ok := seen[item.Symbol]; ok {
			continue
		}
		seen[item.Symbol] = struct{}{}
		result = append(result, item.Symbol)
	}
	return result
}

func renderConfig(results []exchangeSymbols) string {
	byName := map[string][]string{}
	for _, result := range results {
		byName[result.Name] = result.Symbols
	}

	var b strings.Builder
	writeExchange(&b, "binance", byName["binance"])
	writeExchange(&b, "gate", byName["gate"])
	writeExchange(&b, "bitget", byName["bitget"])
	writeExchange(&b, "bybit", byName["bybit"])
	b.WriteString(`[clickhouse]
enabled = true
addr = "localhost:9000"
database = "alphaflow"
username = "alphaflow"
password = "alphaflow"
dial_timeout = "5s"
read_timeout = "30s"
retry_interval = "10s"
retry_batch = 100
max_pending = 100000

[logging]
service = "market-data"
level = "info"
format = "json"
output = "file"
dir = "../../logs/go-service"
filename = "market-data-live-top500.log"
max_size_mb = 100
max_backups = 10
max_age_days = 30
compress = true
`)
	return b.String()
}

func writeExchange(b *strings.Builder, name string, symbols []string) {
	fmt.Fprintf(b, "[%s]\n", name)
	fmt.Fprintf(b, "enabled = %t\n", len(symbols) > 0)
	b.WriteString("symbols = [\n")
	for _, symbol := range symbols {
		fmt.Fprintf(b, "  %q,\n", symbol)
	}
	b.WriteString("]\n\n")
}

func stringField(item map[string]any, key string) string {
	value, ok := item[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.ToUpper(strings.TrimSpace(typed))
	default:
		return strings.ToUpper(strings.TrimSpace(fmt.Sprint(typed)))
	}
}

func firstNumberField(item map[string]any, keys ...string) float64 {
	for _, key := range keys {
		value := numberField(item, key)
		if value > 0 {
			return value
		}
	}
	return 0
}

func numberField(item map[string]any, key string) float64 {
	value, ok := item[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func validLinearSymbol(symbol string) bool {
	return strings.HasSuffix(symbol, "USDT") &&
		!strings.Contains(symbol, "_") &&
		!strings.Contains(symbol, "-")
}

func validGateSymbol(symbol string) bool {
	return strings.HasSuffix(symbol, "_USDT")
}

func firstSymbol(symbols []string) string {
	if len(symbols) == 0 {
		return ""
	}
	return symbols[0]
}

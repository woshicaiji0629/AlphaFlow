package app

import (
	"strings"

	"alphaflow/go-service/market-data/internal/aggregator"
	"alphaflow/go-service/market-data/internal/config"
	"alphaflow/go-service/market-data/internal/health"
	"alphaflow/go-service/market-data/internal/indicator"
)

func aggregationRules(cfg config.Config) []aggregator.Rule {
	rules := []aggregator.Rule{}
	if cfg.Binance.Enabled {
		rules = append(rules, aggregator.Rule{
			Exchange:       "binance",
			Market:         "um",
			Symbols:        cfg.Binance.Symbols,
			SourceInterval: "5m",
			TargetInterval: "10m",
		})
	}
	if cfg.Gate.Enabled {
		rules = append(rules, missingIntervalRules("gate", config.GateSettle(), cfg.Gate.Symbols)...)
	}
	if cfg.Bitget.Enabled {
		rules = append(rules, missingIntervalRules("bitget", strings.ToLower(config.BitgetProductType()), cfg.Bitget.Symbols)...)
	}
	if cfg.Bybit.Enabled {
		rules = append(rules, aggregator.Rule{
			Exchange:       "bybit",
			Market:         config.BybitCategory(),
			Symbols:        cfg.Bybit.Symbols,
			SourceInterval: "5m",
			TargetInterval: "10m",
		})
	}
	return rules
}

func indicatorRules(cfg config.Config) []indicator.Rule {
	rules := []indicator.Rule{}
	if cfg.Binance.Enabled {
		rules = append(rules, indicator.Rule{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   cfg.Binance.Symbols,
			Intervals: withExtraIntervals(config.BinanceIntervals(), "10m"),
		})
	}
	if cfg.Gate.Enabled {
		rules = append(rules, indicator.Rule{
			Exchange:  "gate",
			Market:    config.GateSettle(),
			Symbols:   cfg.Gate.Symbols,
			Intervals: withExtraIntervals(config.GateIntervals(), "3m", "10m", "2h"),
		})
	}
	if cfg.Bitget.Enabled {
		rules = append(rules, indicator.Rule{
			Exchange:  "bitget",
			Market:    strings.ToLower(config.BitgetProductType()),
			Symbols:   cfg.Bitget.Symbols,
			Intervals: withExtraIntervals(config.BitgetIntervals(), "3m", "10m", "2h"),
		})
	}
	if cfg.Bybit.Enabled {
		rules = append(rules, indicator.Rule{
			Exchange:  "bybit",
			Market:    config.BybitCategory(),
			Symbols:   cfg.Bybit.Symbols,
			Intervals: withExtraIntervals(config.BybitIntervals(), "10m"),
		})
	}
	return rules
}

func healthRules(cfg config.Config) []health.Rule {
	rules := []health.Rule{}
	if cfg.Binance.Enabled {
		rules = append(rules, health.Rule{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   cfg.Binance.Symbols,
			Intervals: withExtraIntervals(config.BinanceIntervals(), "10m"),
		})
	}
	if cfg.Gate.Enabled {
		rules = append(rules, health.Rule{
			Exchange:  "gate",
			Market:    config.GateSettle(),
			Symbols:   cfg.Gate.Symbols,
			Intervals: withExtraIntervals(config.GateIntervals(), "3m", "10m", "2h"),
		})
	}
	if cfg.Bitget.Enabled {
		rules = append(rules, health.Rule{
			Exchange:  "bitget",
			Market:    strings.ToLower(config.BitgetProductType()),
			Symbols:   cfg.Bitget.Symbols,
			Intervals: withExtraIntervals(config.BitgetIntervals(), "3m", "10m", "2h"),
		})
	}
	if cfg.Bybit.Enabled {
		rules = append(rules, health.Rule{
			Exchange:  "bybit",
			Market:    config.BybitCategory(),
			Symbols:   cfg.Bybit.Symbols,
			Intervals: withExtraIntervals(config.BybitIntervals(), "10m"),
		})
	}
	return rules
}

func withExtraIntervals(intervals []string, extra ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(intervals)+len(extra))
	for _, interval := range append(intervals, extra...) {
		if _, ok := seen[interval]; ok {
			continue
		}
		seen[interval] = struct{}{}
		result = append(result, interval)
	}
	return result
}

func missingIntervalRules(exchange string, market string, symbols []string) []aggregator.Rule {
	return []aggregator.Rule{
		{
			Exchange:       exchange,
			Market:         market,
			Symbols:        symbols,
			SourceInterval: "1m",
			TargetInterval: "3m",
		},
		{
			Exchange:       exchange,
			Market:         market,
			Symbols:        symbols,
			SourceInterval: "5m",
			TargetInterval: "10m",
		},
		{
			Exchange:       exchange,
			Market:         market,
			Symbols:        symbols,
			SourceInterval: "1h",
			TargetInterval: "2h",
		},
	}
}

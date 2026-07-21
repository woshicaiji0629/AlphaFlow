package swing

import (
	"context"
	"fmt"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/signalresearch"
)

type Summary struct {
	DefinitionVersion string         `json:"definition_version"`
	StartTimeMS       int64          `json:"start_time_ms"`
	EndTimeMS         int64          `json:"end_time_ms"`
	Total             int            `json:"total"`
	Buckets           map[string]int `json:"buckets"`
	UpSwings          int            `json:"up_swings"`
	DownSwings        int            `json:"down_swings"`
}

func Run(ctx context.Context, configPath string, minimumMovePoints, reversalPoints float64) (Summary, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return Summary{}, fmt.Errorf("load config: %w", err)
	}
	if len(cfg.Data.Symbols) != 1 {
		return Summary{}, fmt.Errorf("market swing research requires exactly one symbol")
	}
	start, err := config.StartTime(cfg)
	if err != nil {
		return Summary{}, err
	}
	end, err := config.EndTime(cfg)
	if err != nil {
		return Summary{}, err
	}
	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return Summary{}, err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return Summary{}, err
	}
	marketStore, err := clickhousemarket.NewStore(ctx, clickhousemarket.Options{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database,
		Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password,
		DialTimeout: dialTimeout, ReadTimeout: readTimeout, SkipSchemaInit: true,
	})
	if err != nil {
		return Summary{}, err
	}
	defer marketStore.Close()
	klineReader, err := reader.New(marketStore)
	if err != nil {
		return Summary{}, err
	}
	series, err := klineReader.ReadKlines(ctx, reader.Request{
		Exchange: cfg.Data.Exchange, Market: cfg.Data.Market, Symbol: cfg.Data.Symbols[0],
		Interval: cfg.Data.Interval, Start: start.UnixMilli(), End: end.UnixMilli(), WarmupBars: 0,
	})
	if err != nil {
		return Summary{}, err
	}
	review, err := signalresearch.ReviewSwings(series.Klines, nil, nil, nil, signalresearch.SwingReviewConfig{
		MinimumMovePoints: minimumMovePoints, ReversalPoints: reversalPoints,
	})
	if err != nil {
		return Summary{}, err
	}
	swings := signalresearch.BuildMarketSwings(
		cfg.Data.Exchange, cfg.Data.Market, cfg.Data.Symbols[0], cfg.Data.Interval, review,
	)
	researchStore, err := signalresearch.NewStore(ctx, signalresearch.StoreOptions{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database,
		Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password,
		DialTimeout: dialTimeout, ReadTimeout: readTimeout,
	})
	if err != nil {
		return Summary{}, err
	}
	defer researchStore.Close()
	if err := researchStore.SaveMarketSwings(ctx, swings, 500); err != nil {
		return Summary{}, err
	}
	result := Summary{
		DefinitionVersion: signalresearch.MarketSwingDefinitionVersion,
		StartTimeMS:       start.UnixMilli(), EndTimeMS: end.UnixMilli(), Total: len(swings),
		Buckets: map[string]int{"30_60": 0, "60_100": 0, "100_150": 0, "150_plus": 0},
	}
	for _, swing := range swings {
		result.Buckets[swing.MoveBucket]++
		if swing.Side == "buy" {
			result.UpSwings++
		} else {
			result.DownSwings++
		}
	}
	return result, nil
}

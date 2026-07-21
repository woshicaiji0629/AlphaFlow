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
	DefinitionVersion string                            `json:"definition_version"`
	StartTimeMS       int64                             `json:"start_time_ms"`
	EndTimeMS         int64                             `json:"end_time_ms"`
	Total             int                               `json:"total"`
	Buckets           map[string]int                    `json:"buckets"`
	UpSwings          int                               `json:"up_swings"`
	DownSwings        int                               `json:"down_swings"`
	Opportunities     []signalresearch.SwingOpportunity `json:"opportunities,omitempty"`
}

func Run(ctx context.Context, configPath string, reviewConfig signalresearch.SwingReviewConfig) (Summary, error) {
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
	review, err := signalresearch.ReviewSwings(series.Klines, nil, nil, nil, reviewConfig)
	if err != nil {
		return Summary{}, err
	}
	swings := signalresearch.BuildMarketSwings(cfg.Data.Exchange, cfg.Data.Market, cfg.Data.Symbols[0], cfg.Data.Interval, review)
	if review.ThresholdMode == signalresearch.SwingThresholdPoints {
		researchStore, storeErr := signalresearch.NewStore(ctx, signalresearch.StoreOptions{
			Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database,
			Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password,
			DialTimeout: dialTimeout, ReadTimeout: readTimeout,
		})
		if storeErr != nil {
			return Summary{}, storeErr
		}
		defer researchStore.Close()
		if err := researchStore.SaveMarketSwings(ctx, swings, 500); err != nil {
			return Summary{}, err
		}
	}
	buckets := map[string]int{}
	for _, opportunity := range review.Opportunities {
		buckets[opportunity.MoveBucket]++
	}
	result := Summary{
		DefinitionVersion: string(review.ThresholdMode) + "-v1",
		StartTimeMS:       start.UnixMilli(), EndTimeMS: end.UnixMilli(), Total: len(review.Opportunities), Buckets: buckets,
		UpSwings: review.UpSwings, DownSwings: review.DownSwings,
	}
	if review.ThresholdMode == signalresearch.SwingThresholdPoints {
		result.DefinitionVersion = signalresearch.MarketSwingDefinitionVersion
	} else {
		result.Opportunities = review.Opportunities
	}
	return result, nil
}

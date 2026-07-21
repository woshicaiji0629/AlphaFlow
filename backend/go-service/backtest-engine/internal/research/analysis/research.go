package analysis

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/backtest-engine/internal/simulator"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

type versionRun struct {
	analyzer    marketregime.Analyzer
	fingerprint string
	items       []marketregime.Result
}

type Summary struct {
	StartTimeMS int64          `json:"start_time_ms"`
	EndTimeMS   int64          `json:"end_time_ms"`
	Versions    map[string]int `json:"versions"`
}

func Run(ctx context.Context, configPath string) (Summary, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return Summary{}, fmt.Errorf("load config: %w", err)
	}
	if len(cfg.Data.Symbols) != 1 {
		return Summary{}, fmt.Errorf("market analysis research requires exactly one symbol")
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
	dataset, err := klineReader.ReadDataset(ctx, reader.DatasetRequest{
		Exchange: cfg.Data.Exchange, Market: cfg.Data.Market, Symbols: cfg.Data.Symbols,
		Interval: cfg.Data.Interval, ConfirmIntervals: cfg.Data.ConfirmIntervals,
		Start: start.UnixMilli(), End: end.UnixMilli(), WarmupBars: cfg.Data.WarmupBars,
	})
	if err != nil {
		return Summary{}, err
	}
	target := strategy.Target{
		Exchange: cfg.Data.Exchange, Market: cfg.Data.Market, Symbol: cfg.Data.Symbols[0],
		Interval: cfg.Data.Interval, Scope: strategy.PositionScopeBacktest,
	}
	builder, err := simulator.NewSnapshotBuilder(simulator.SnapshotBuilderOptions{
		Dataset: dataset, Target: target, Interval: cfg.Data.Interval,
		ConfirmIntervals: cfg.Data.ConfirmIntervals, IndicatorOptions: indicatorcalc.DefaultOptions(),
		CalculationWindow: int(cfg.Data.WarmupBars), IndicatorBatchSize: cfg.Data.IndicatorBatchSize,
		IndicatorConcurrency: cfg.Data.IndicatorConcurrency,
	})
	if err != nil {
		return Summary{}, err
	}
	versions := make([]versionRun, 0, 3)
	for _, version := range []marketregime.Version{marketregime.VersionV4, marketregime.VersionV5, marketregime.VersionV6} {
		analyzer, err := marketregime.NewAnalyzer(version, marketregime.DefaultConfig())
		if err != nil {
			return Summary{}, err
		}
		fingerprint, err := configFingerprint(version)
		if err != nil {
			return Summary{}, err
		}
		versions = append(versions, versionRun{analyzer: analyzer, fingerprint: fingerprint})
	}
	iterator, err := builder.Iterator(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer iterator.Close()
	for {
		item, ok, err := iterator.Next(ctx)
		if err != nil {
			return Summary{}, err
		}
		if !ok {
			break
		}
		snapshot, ok := item.Snapshots[cfg.Data.Interval]
		if !ok {
			return Summary{}, fmt.Errorf("entry snapshot %s missing", cfg.Data.Interval)
		}
		for index := range versions {
			observation, ready, err := versions[index].analyzer.Analyze(snapshot)
			if err != nil {
				return Summary{}, fmt.Errorf("analyze market regime %s: %w", versions[index].analyzer.Version(), err)
			}
			if ready && snapshot.Current.OpenTime >= start.UnixMilli() && snapshot.Current.OpenTime < end.UnixMilli() {
				versions[index].items = append(versions[index].items, observation)
			}
		}
	}
	researchStore, err := signalresearch.NewStore(ctx, signalresearch.StoreOptions{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database,
		Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password,
		DialTimeout: dialTimeout, ReadTimeout: readTimeout,
	})
	if err != nil {
		return Summary{}, err
	}
	defer researchStore.Close()
	result := Summary{StartTimeMS: start.UnixMilli(), EndTimeMS: end.UnixMilli(), Versions: map[string]int{}}
	for _, version := range versions {
		if err := researchStore.SaveMarketAnalysisObservations(
			ctx, cfg.Data.Exchange, cfg.Data.Market, cfg.Data.Symbols[0], cfg.Data.Interval,
			version.fingerprint, version.items, 1000,
		); err != nil {
			return Summary{}, err
		}
		result.Versions[string(version.analyzer.Version())] = len(version.items)
	}
	return result, nil
}

func configFingerprint(version marketregime.Version) (string, error) {
	var value any
	switch version {
	case marketregime.VersionV4:
		value = marketregime.DefaultV4Config()
	case marketregime.VersionV5:
		value = marketregime.DefaultV5Config()
	case marketregime.VersionV6:
		value = marketregime.DefaultV6Config()
	default:
		return "", fmt.Errorf("unsupported fingerprint version %q", version)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal %s config: %w", version, err)
	}
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", digest[:16]), nil
}

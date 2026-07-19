package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/backtest-engine/internal/simulator"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategies/supertrend"
	"alphaflow/go-service/pkg/strategy"
)

func main() {
	configPath := flag.String("config", "configs/supertrend-signal-research.ethusdt-20250801-20251201.toml", "research config path")
	fixedStops := flag.String("fixed-stops", "50,70,100,150", "fixed stop margin percentages")
	atrStops := flag.String("atr-stops", "1,1.5,2,2.5", "ATR stop multipliers")
	takeProfits := flag.String("take-profits", "30,50,75,100,150,200,300,500", "take profit margin percentages")
	horizon := flag.Duration("horizon", 12*time.Hour, "maximum observation horizon")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *configPath, *fixedStops, *atrStops, *takeProfits, *horizon); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, configPath string, fixedText string, atrText string, takeProfitText string, horizon time.Duration) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load research config: %w", err)
	}
	if !cfg.ClickHouse.Enabled {
		return fmt.Errorf("clickhouse must be enabled")
	}
	if len(cfg.Data.Symbols) != 1 {
		return fmt.Errorf("signal research requires exactly one symbol")
	}
	fixed, err := parsePositiveList("fixed-stops", fixedText)
	if err != nil {
		return err
	}
	atr, err := parsePositiveList("atr-stops", atrText)
	if err != nil {
		return err
	}
	takeProfits, err := parsePositiveList("take-profits", takeProfitText)
	if err != nil {
		return err
	}
	startTime, err := config.StartTime(cfg)
	if err != nil {
		return err
	}
	endTime, err := config.EndTime(cfg)
	if err != nil {
		return err
	}
	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return err
	}
	marketStore, err := clickhousemarket.NewStore(ctx, clickhousemarket.Options{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database, Username: cfg.ClickHouse.Username,
		Password: cfg.ClickHouse.Password, DialTimeout: dialTimeout, ReadTimeout: readTimeout,
	})
	if err != nil {
		return err
	}
	defer marketStore.Close()
	klineReader, err := reader.New(marketStore)
	if err != nil {
		return err
	}
	dataset, err := klineReader.ReadDataset(ctx, reader.DatasetRequest{
		Exchange: cfg.Data.Exchange, Market: cfg.Data.Market, Symbols: cfg.Data.Symbols,
		Interval: cfg.Data.Interval, ConfirmIntervals: cfg.Data.ConfirmIntervals,
		Start: startTime.UnixMilli(), End: endTime.Add(horizon).UnixMilli(), WarmupBars: cfg.Data.WarmupBars,
	})
	if err != nil {
		return err
	}
	replay, err := signalresearch.New(signalresearch.Config{
		RunID: cfg.Runtime.RunID, Leverage: cfg.Sizing.Leverage, Horizon: horizon,
		FixedStopMargin: fixed, ATRStopMultipliers: atr, TakeProfitMargin: takeProfits,
	})
	if err != nil {
		return err
	}
	target := strategy.Target{
		Exchange: cfg.Data.Exchange, Market: cfg.Data.Market, Symbol: cfg.Data.Symbols[0],
		Interval: cfg.Data.Interval, Scope: strategy.PositionScopeBacktest, RunID: cfg.Runtime.RunID,
	}
	builder, err := simulator.NewSnapshotBuilder(simulator.SnapshotBuilderOptions{
		Dataset: dataset, Target: target, Interval: cfg.Data.Interval, ConfirmIntervals: cfg.Data.ConfirmIntervals,
		IndicatorOptions: indicatorcalc.DefaultOptions(), CalculationWindow: int(cfg.Data.WarmupBars),
		IndicatorBatchSize: cfg.Data.IndicatorBatchSize, IndicatorConcurrency: cfg.Data.IndicatorConcurrency,
	})
	if err != nil {
		return err
	}
	iterator, err := builder.Iterator(ctx)
	if err != nil {
		return err
	}
	defer iterator.Close()
	for {
		item, ok, err := iterator.Next(ctx)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		snapshot, ok := item.Snapshots[cfg.Data.Interval]
		if !ok {
			return fmt.Errorf("entry snapshot %s missing", cfg.Data.Interval)
		}
		if err := replay.Advance(snapshot.Current); err != nil {
			return err
		}
		if snapshot.Current.OpenTime >= endTime.UnixMilli() {
			continue
		}
		for _, side := range []strategy.SignalSide{strategy.SignalSideBuy, strategy.SignalSideSell} {
			sources := supertrend.ResearchTriggerSources(snapshot.Window, side)
			if len(sources) == 0 {
				continue
			}
			if err := replay.AddSignal(snapshot, side, sources); err != nil {
				return err
			}
		}
	}
	replay.Finish()
	signals, outcomes := replay.Results()
	researchStore, err := signalresearch.NewStore(ctx, signalresearch.StoreOptions{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database, Username: cfg.ClickHouse.Username,
		Password: cfg.ClickHouse.Password, DialTimeout: dialTimeout, ReadTimeout: readTimeout,
	})
	if err != nil {
		return err
	}
	defer researchStore.Close()
	if err := researchStore.SaveSignals(ctx, signals, 250); err != nil {
		return err
	}
	if err := researchStore.SaveOutcomes(ctx, outcomes, 1000); err != nil {
		return err
	}
	log.Printf("signal research completed run_id=%s signals=%d outcomes=%d", cfg.Runtime.RunID, len(signals), len(outcomes))
	return nil
}

func parsePositiveList(name string, text string) ([]float64, error) {
	parts := strings.Split(text, ",")
	values := make([]float64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("%s contains invalid positive number %q", name, part)
		}
		values = append(values, value)
	}
	return values, nil
}

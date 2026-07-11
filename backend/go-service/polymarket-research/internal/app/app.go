package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"alphaflow/go-service/polymarket-research/internal/clob"
	"alphaflow/go-service/polymarket-research/internal/config"
	"alphaflow/go-service/polymarket-research/internal/gamma"
	"alphaflow/go-service/polymarket-research/internal/model"
	"alphaflow/go-service/polymarket-research/internal/rtds"
	"alphaflow/go-service/polymarket-research/internal/store"
)

type marketStore interface {
	UpsertMarkets(context.Context, []model.Market) error
	WriteBookTick(context.Context, model.BookTick) error
	WriteTrade(context.Context, model.Trade) error
	WriteReferencePrice(context.Context, model.ReferencePrice) error
	WriteResolution(context.Context, model.Resolution) error
	Close() error
}
type nilStore struct{}

func (nilStore) UpsertMarkets(context.Context, []model.Market) error             { return nil }
func (nilStore) Close() error                                                    { return nil }
func (nilStore) WriteBookTick(context.Context, model.BookTick) error             { return nil }
func (nilStore) WriteTrade(context.Context, model.Trade) error                   { return nil }
func (nilStore) WriteReferencePrice(context.Context, model.ReferencePrice) error { return nil }
func (nilStore) WriteResolution(context.Context, model.Resolution) error         { return nil }

func Run(ctx context.Context, path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	pollInterval, _ := config.PollInterval(cfg)
	client := gamma.New(gamma.Options{BaseURL: cfg.Gamma.BaseURL, PageSize: cfg.Gamma.PageSize})
	var target marketStore = nilStore{}
	if cfg.ClickHouse.Enabled {
		dialTimeout, _ := config.ClickHouseDialTimeout(cfg)
		readTimeout, _ := config.ClickHouseReadTimeout(cfg)
		clickhouseStore, openErr := store.NewClickHouse(ctx, store.Options{Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database, Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password, DialTimeout: dialTimeout, ReadTimeout: readTimeout})
		err = openErr
		if err != nil {
			return err
		}
		flushInterval, _ := config.BatchFlushInterval(cfg)
		target = store.NewBatchStore(clickhouseStore, cfg.Batch.MaxSize, cfg.Batch.ChannelSize, flushInterval)
	}
	defer target.Close()
	reconnect, _ := config.ReconnectWait(cfg)
	clobClient := clob.New(cfg.Realtime.CLOBURL, target, reconnect)
	rtdsClient := rtds.New(cfg.Realtime.RTDSURL, target, reconnect)
	if err := syncOnce(ctx, client, target, clobClient, cfg.Research.Symbols, cfg.Research.Durations); err != nil {
		return err
	}
	var streamWG sync.WaitGroup
	streamWG.Add(2)
	go func() {
		defer streamWG.Done()
		if err := clobClient.Run(ctx); err != nil {
			slog.Error("clob stream stopped", "error", err)
		}
	}()
	go func() {
		defer streamWG.Done()
		if err := rtdsClient.Run(ctx); err != nil {
			slog.Error("rtds stream stopped", "error", err)
		}
	}()
	defer streamWG.Wait()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	healthInterval, _ := config.HealthInterval(cfg)
	healthTicker := time.NewTicker(healthInterval)
	defer healthTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := syncOnce(ctx, client, target, clobClient, cfg.Research.Symbols, cfg.Research.Durations); err != nil {
				if ctx.Err() == nil {
					slog.Error("sync polymarket markets failed", "error", err)
				}
			}
		case <-healthTicker.C:
			clobConnected, clobLast, clobReconnects := clobClient.Stats()
			rtdsConnected, rtdsLast, rtdsReconnects := rtdsClient.Stats()
			if stats, ok := target.(interface{ Stats() (int64, int64) }); ok {
				pending, flushErrors := stats.Stats()
				slog.Info("polymarket research health", "pending_events", pending, "batch_flush_errors", flushErrors, "clob_connected", clobConnected, "clob_last_message_ms", clobLast, "clob_reconnects", clobReconnects, "rtds_connected", rtdsConnected, "rtds_last_message_ms", rtdsLast, "rtds_reconnects", rtdsReconnects)
			}
		}
	}
}

type discoverer interface {
	Discover(context.Context, []string, []string) ([]model.Market, error)
}

type marketUpdater interface{ UpdateMarkets([]model.Market) }

func syncOnce(ctx context.Context, client discoverer, target marketStore, updater marketUpdater, symbols, durations []string) error {
	markets, err := client.Discover(ctx, symbols, durations)
	if err != nil {
		return err
	}
	if err := target.UpsertMarkets(ctx, markets); err != nil {
		return err
	}
	updater.UpdateMarkets(markets)
	slog.Info("synced polymarket markets", "count", len(markets))
	return nil
}

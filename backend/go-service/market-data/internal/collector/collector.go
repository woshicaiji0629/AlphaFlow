package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/exchange"
	"alphaflow/go-service/market-data/internal/model"
)

type Store interface {
	LastOpenTime(
		ctx context.Context,
		exchange string,
		market string,
		symbol string,
		interval string,
	) (int64, bool, error)
	UpsertKline(ctx context.Context, kline model.Kline) error
	SetLastPrice(ctx context.Context, price model.LastPrice) error
	SetMarkPrice(ctx context.Context, price model.MarkPrice) error
	SetBookTicker(ctx context.Context, ticker model.BookTicker) error
	SetOpenInterest(ctx context.Context, interest model.OpenInterest) error
	AddLiquidation(ctx context.Context, liquidation model.Liquidation, limit int64) error
	SetMarketStatus(ctx context.Context, status model.MarketStatus) error
}

type Collector struct {
	options Options
	rest    exchange.RESTClient
	ws      exchange.WSClient
	store   Store
	now     func() time.Time
}

type Options struct {
	Symbols              []string
	Intervals            []string
	RESTLimit            int
	ReconnectDelay       time.Duration
	LiquidationLimit     int64
	PollOpenInterest     bool
	OpenInterestInterval time.Duration
	MarkPriceInterval    string
}

func New(
	options Options,
	rest exchange.RESTClient,
	ws exchange.WSClient,
	store Store,
) *Collector {
	return &Collector{
		options: options,
		rest:    rest,
		ws:      ws,
		store:   store,
		now:     time.Now,
	}
}

func (c *Collector) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)

	go func() {
		errCh <- c.runWebSocketLoop(ctx)
	}()

	go func() {
		errCh <- c.runPollingLoop(ctx)
	}()

	err := <-errCh
	if err != nil && ctx.Err() == nil {
		c.setMarketUnavailable(ctx, err.Error())
	}
	cancel()
	if err != nil {
		return err
	}

	return <-errCh
}

func (c *Collector) runWebSocketLoop(ctx context.Context) error {
	streams := c.streams()
	reconnectDelay := c.options.ReconnectDelay
	for {
		if err := c.Backfill(ctx); err != nil {
			return err
		}

		slog.Info("starting websocket", "streams", len(streams))
		err := c.ws.Run(ctx, streams, c)
		if ctx.Err() != nil {
			return nil
		}
		slog.Warn("websocket stopped", "error", err, "reconnect_delay", reconnectDelay)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(reconnectDelay):
		}
	}
}

func (c *Collector) runPollingLoop(ctx context.Context) error {
	if !c.options.PollOpenInterest {
		<-ctx.Done()
		return nil
	}

	interval := c.options.OpenInterestInterval

	c.pollOpenInterest(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.pollOpenInterest(ctx)
		}
	}
}

func (c *Collector) pollOpenInterest(ctx context.Context) {
	for _, symbol := range c.options.Symbols {
		interest, err := c.rest.FetchOpenInterest(ctx, symbol)
		if err != nil {
			slog.Error("fetch open interest failed", "symbol", symbol, "error", err)
			continue
		}
		if err := c.store.SetOpenInterest(ctx, interest); err != nil {
			slog.Error("store open interest failed", "symbol", symbol, "error", err)
			continue
		}
		slog.Debug("updated open interest", "symbol", symbol)
	}
}

func (c *Collector) Backfill(ctx context.Context) error {
	for _, symbol := range c.options.Symbols {
		for _, interval := range c.options.Intervals {
			startTime, err := c.nextStartTime(ctx, symbol, interval)
			if err != nil {
				return err
			}
			if !c.hasClosedWindowToBackfill(interval, startTime) {
				slog.Debug("skip backfill without closed window", "symbol", symbol, "interval", interval, "start_time", startTime)
				continue
			}

			klines, err := c.rest.FetchKlines(
				ctx,
				symbol,
				interval,
				c.options.RESTLimit,
				startTime,
			)
			if err != nil {
				return fmt.Errorf("backfill %s %s: %w", symbol, interval, err)
			}
			for _, kline := range klines {
				if err := c.store.UpsertKline(ctx, kline); err != nil {
					return fmt.Errorf("store %s %s %d: %w", symbol, interval, kline.OpenTime, err)
				}
			}
			c.setMarketAvailable(ctx)
			slog.Info("backfilled klines", "symbol", symbol, "interval", interval, "count", len(klines))
		}
	}
	return nil
}

func (c *Collector) hasClosedWindowToBackfill(interval string, startTime int64) bool {
	if startTime <= 0 {
		return true
	}
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return true
	}
	return startTime <= c.now().UnixMilli()-intervalMillis
}

func (c *Collector) nextStartTime(ctx context.Context, symbol string, interval string) (int64, error) {
	lastOpenTime, ok, err := c.store.LastOpenTime(
		ctx,
		c.rest.Exchange(),
		c.rest.Market(),
		symbol,
		interval,
	)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return 0, err
	}
	return lastOpenTime + intervalMillis, nil
}

func (c *Collector) HandleKline(ctx context.Context, kline model.Kline) error {
	if kline.Exchange == "" || kline.Market == "" || kline.Symbol == "" || kline.Interval == "" {
		return errors.New("invalid empty kline identity")
	}
	if err := c.store.UpsertKline(ctx, kline); err != nil {
		return err
	}
	c.setMarketAvailable(ctx)
	return nil
}

func (c *Collector) HandleLastPrice(ctx context.Context, price model.LastPrice) error {
	if price.Exchange == "" || price.Market == "" || price.Symbol == "" {
		return errors.New("invalid empty last price identity")
	}
	if err := c.store.SetLastPrice(ctx, price); err != nil {
		return err
	}
	c.setMarketAvailable(ctx)
	return nil
}

func (c *Collector) HandleMarkPrice(ctx context.Context, price model.MarkPrice) error {
	if price.Exchange == "" || price.Market == "" || price.Symbol == "" {
		return errors.New("invalid empty mark price identity")
	}
	if err := c.store.SetMarkPrice(ctx, price); err != nil {
		return err
	}
	c.setMarketAvailable(ctx)
	return nil
}

func (c *Collector) HandleBookTicker(ctx context.Context, ticker model.BookTicker) error {
	if ticker.Exchange == "" || ticker.Market == "" || ticker.Symbol == "" {
		return errors.New("invalid empty book ticker identity")
	}
	if err := c.store.SetBookTicker(ctx, ticker); err != nil {
		return err
	}
	c.setMarketAvailable(ctx)
	return nil
}

func (c *Collector) HandleOpenInterest(ctx context.Context, interest model.OpenInterest) error {
	if interest.Exchange == "" || interest.Market == "" || interest.Symbol == "" {
		return errors.New("invalid empty open interest identity")
	}
	if err := c.store.SetOpenInterest(ctx, interest); err != nil {
		return err
	}
	c.setMarketAvailable(ctx)
	return nil
}

func (c *Collector) HandleLiquidation(ctx context.Context, liquidation model.Liquidation) error {
	if liquidation.Exchange == "" || liquidation.Market == "" || liquidation.Symbol == "" {
		return errors.New("invalid empty liquidation identity")
	}
	if err := c.store.AddLiquidation(ctx, liquidation, c.options.LiquidationLimit); err != nil {
		return err
	}
	c.setMarketAvailable(ctx)
	return nil
}

func (c *Collector) setMarketAvailable(ctx context.Context) {
	c.setMarketStatus(ctx, true, "")
}

func (c *Collector) setMarketUnavailable(ctx context.Context, reason string) {
	c.setMarketStatus(ctx, false, reason)
}

func (c *Collector) setMarketStatus(ctx context.Context, available bool, reason string) {
	if ctx.Err() != nil {
		return
	}
	if err := c.store.SetMarketStatus(ctx, model.MarketStatus{
		Exchange:  c.rest.Exchange(),
		Market:    c.rest.Market(),
		Available: available,
		Reason:    reason,
		UpdatedAt: time.Now().UnixMilli(),
	}); err != nil {
		slog.Error(
			"set market status failed",
			"exchange", c.rest.Exchange(),
			"market", c.rest.Market(),
			"available", available,
			"error", err,
		)
	}
}

func (c *Collector) streams() []exchange.Stream {
	streams := make(
		[]exchange.Stream,
		0,
		len(c.options.Symbols)*(len(c.options.Intervals)+4),
	)
	for _, symbol := range c.options.Symbols {
		for _, interval := range c.options.Intervals {
			streams = append(streams, exchange.Stream{
				Symbol:   symbol,
				Interval: interval,
				Type:     exchange.StreamTypeKline,
			})
		}
		streams = append(streams, exchange.Stream{
			Symbol: symbol,
			Type:   exchange.StreamTypeAggTrade,
		})
		streams = append(streams, exchange.Stream{
			Symbol:   symbol,
			Interval: c.options.MarkPriceInterval,
			Type:     exchange.StreamTypeMarkPrice,
		})
		streams = append(streams, exchange.Stream{
			Symbol: symbol,
			Type:   exchange.StreamTypeBookTicker,
		})
		streams = append(streams, exchange.Stream{
			Symbol: symbol,
			Type:   exchange.StreamTypeForceOrder,
		})
	}
	return streams
}

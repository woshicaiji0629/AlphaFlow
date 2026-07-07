package collector

import (
	"context"
	"log/slog"
	"time"
)

func (c *Collector) runPollingLoop(ctx context.Context) error {
	if !c.options.PollOpenInterest {
		<-ctx.Done()
		return nil
	}

	interval := c.options.OpenInterestInterval

	if err := c.waitOpenInterestStartup(ctx); err != nil {
		return nil
	}
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

func (c *Collector) waitOpenInterestStartup(ctx context.Context) error {
	timer := time.NewTimer(openInterestStartupDelay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Collector) pollOpenInterest(ctx context.Context) {
	for _, symbol := range c.openInterestSymbols() {
		if ctx.Err() != nil {
			return
		}
		interest, err := c.rest.FetchOpenInterest(ctx, symbol)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("fetch open interest failed", "symbol", symbol, "error", err)
			continue
		}
		if err := c.store.SetOpenInterest(ctx, interest); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("store open interest failed", "symbol", symbol, "error", err)
			continue
		}
		slog.Debug("updated open interest", "symbol", symbol)
	}
}

func (c *Collector) openInterestSymbols() []string {
	if len(c.options.Symbols) <= maxOpenInterestSymbols {
		return c.options.Symbols
	}
	return c.options.Symbols[:maxOpenInterestSymbols]
}

package collector

import (
	"context"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

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

func (c *Collector) setWebSocketConnected(
	ctx context.Context,
	shard string,
	reconnectCount int64,
	consecutiveFailures int64,
	startedAt time.Time,
	streamCount int,
	connectionCount int,
) {
	c.setWebSocketStatus(ctx, model.WebSocketStatus{
		Exchange:            c.rest.Exchange(),
		Market:              c.rest.Market(),
		Shard:               shard,
		Connected:           true,
		LastStartedAt:       startedAt.UnixMilli(),
		StreamCount:         streamCount,
		ConnectionCount:     connectionCount,
		ReconnectCount:      reconnectCount,
		ConsecutiveFailures: consecutiveFailures,
		UpdatedAt:           c.now().UnixMilli(),
	})
}

func (c *Collector) setWebSocketDisconnected(
	ctx context.Context,
	shard string,
	err error,
	reconnectCount int64,
	consecutiveFailures int64,
	streamCount int,
	connectionCount int,
) {
	status := model.WebSocketStatus{
		Exchange:            c.rest.Exchange(),
		Market:              c.rest.Market(),
		Shard:               shard,
		Connected:           false,
		LastStoppedAt:       c.now().UnixMilli(),
		StreamCount:         streamCount,
		ConnectionCount:     connectionCount,
		ReconnectCount:      reconnectCount,
		ConsecutiveFailures: consecutiveFailures,
		UpdatedAt:           c.now().UnixMilli(),
	}
	if err != nil {
		status.LastError = err.Error()
	}
	c.setWebSocketStatus(ctx, status)
}

func (c *Collector) setWebSocketStatus(ctx context.Context, status model.WebSocketStatus) {
	if ctx.Err() != nil {
		return
	}
	if err := c.store.SetWebSocketStatus(ctx, status); err != nil {
		slog.Error(
			"set websocket status failed",
			"exchange", status.Exchange,
			"market", status.Market,
			"connected", status.Connected,
			"error", err,
		)
	}
}

package collector

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"alphaflow/go-service/market-data/internal/exchange"
)

func (c *Collector) runWebSocketLoop(ctx context.Context) error {
	if c.ws == nil {
		return errors.New("nil websocket client")
	}
	streams := c.streams()
	shards := distributeStreams(streams, c.webSocketConnections(len(streams)))
	if len(shards) == 0 {
		return errors.New("no websocket streams")
	}

	var wg sync.WaitGroup
	for index, shard := range shards {
		wg.Add(1)
		go func(shardIndex int, shardStreams []exchange.Stream) {
			defer wg.Done()
			c.runWebSocketShardLoop(ctx, shardIndex, len(shards), shardStreams)
		}(index, shard)
	}

	<-ctx.Done()
	wg.Wait()
	return nil
}

func (c *Collector) runWebSocketShardLoop(
	ctx context.Context,
	shardIndex int,
	shardCount int,
	streams []exchange.Stream,
) {
	baseReconnectDelay := c.options.ReconnectDelay
	var reconnectCount int64
	var consecutiveFailures int64
	shard := strconv.Itoa(shardIndex)

	for {
		startedAt := c.now()
		c.setWebSocketConnected(ctx, shard, reconnectCount, consecutiveFailures, startedAt, len(streams), shardCount)
		slog.Info("starting websocket", "shard", shard, "shards", shardCount, "streams", len(streams))
		err := c.ws.Run(ctx, streams, c)
		if ctx.Err() != nil {
			c.setWebSocketDisconnected(ctx, shard, nil, reconnectCount, consecutiveFailures, len(streams), shardCount)
			return
		}
		if c.now().Sub(startedAt) >= reconnectBackoffReset {
			consecutiveFailures = 0
		}
		consecutiveFailures++
		reconnectCount++
		reconnectDelay := nextReconnectDelay(baseReconnectDelay, consecutiveFailures)
		c.setWebSocketDisconnected(ctx, shard, err, reconnectCount, consecutiveFailures, len(streams), shardCount)
		slog.Warn(
			"websocket stopped",
			"shard", shard,
			"shards", shardCount,
			"error", err,
			"reconnect_delay", reconnectDelay,
			"reconnect_count", reconnectCount,
			"consecutive_failures", consecutiveFailures,
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func (c *Collector) webSocketConnections(streamCount int) int {
	return webSocketConnections(c.options, streamCount)
}

func webSocketConnections(options Options, streamCount int) int {
	if streamCount <= 0 {
		return 0
	}
	if options.WebSocketConnections > 0 {
		if options.WebSocketConnections > streamCount {
			return streamCount
		}
		return options.WebSocketConnections
	}
	return (streamCount + defaultMaxStreams - 1) / defaultMaxStreams
}

func distributeStreams(streams []exchange.Stream, connections int) [][]exchange.Stream {
	if len(streams) == 0 {
		return nil
	}
	if connections <= 0 {
		connections = 1
	}
	if connections > len(streams) {
		connections = len(streams)
	}

	shards := make([][]exchange.Stream, 0, connections)
	baseSize := len(streams) / connections
	remainder := len(streams) % connections
	start := 0
	for index := 0; index < connections; index++ {
		size := baseSize
		if index < remainder {
			size++
		}
		end := start + size
		shards = append(shards, streams[start:end])
		start = end
	}
	return shards
}

func nextReconnectDelay(base time.Duration, consecutiveFailures int64) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if consecutiveFailures <= 1 {
		return clampReconnectDelay(base)
	}

	delay := base
	for i := int64(1); i < consecutiveFailures; i++ {
		if delay >= maxReconnectDelay/2 {
			return maxReconnectDelay
		}
		delay *= 2
	}
	return clampReconnectDelay(delay)
}

func clampReconnectDelay(delay time.Duration) time.Duration {
	if delay > maxReconnectDelay {
		return maxReconnectDelay
	}
	return delay
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

package clob

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"alphaflow/go-service/polymarket-research/internal/model"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type Sink interface {
	WriteBookTick(context.Context, model.BookTick) error
	WriteTrade(context.Context, model.Trade) error
	WriteResolution(context.Context, model.Resolution) error
}

type tokenInfo struct{ MarketID, Outcome string }
type Client struct {
	url         string
	sink        Sink
	reconnect   time.Duration
	now         func() time.Time
	mu          sync.RWMutex
	tokens      map[string]tokenInfo
	changed     chan struct{}
	connected   atomic.Bool
	lastMessage atomic.Int64
	reconnects  atomic.Int64
}

func New(url string, sink Sink, reconnect time.Duration) *Client {
	return &Client{url: url, sink: sink, reconnect: reconnect, now: time.Now, tokens: map[string]tokenInfo{}, changed: make(chan struct{}, 1)}
}

func (c *Client) UpdateMarkets(markets []model.Market) {
	next := map[string]tokenInfo{}
	for _, market := range markets {
		now := c.now().UnixMilli()
		if market.Closed || !market.AcceptingOrders || market.EndTimeMS <= now || market.StartTimeMS > now+int64(15*time.Minute/time.Millisecond) {
			continue
		}
		next[market.YesTokenID] = tokenInfo{market.MarketID, "up"}
		next[market.NoTokenID] = tokenInfo{market.MarketID, "down"}
	}
	c.mu.Lock()
	changed := !sameTokens(c.tokens, next)
	c.tokens = next
	c.mu.Unlock()
	if !changed {
		return
	}
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

func sameTokens(a, b map[string]tokenInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for token, info := range a {
		if b[token] != info {
			return false
		}
	}
	return true
}

func (c *Client) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := c.runOnce(ctx); err != nil && ctx.Err() == nil {
			c.connected.Store(false)
			c.reconnects.Add(1)
			slog.Warn("clob websocket disconnected", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(c.reconnect):
			}
		}
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	select {
	case <-c.changed:
	default:
	}
	conn, _, err := websocket.Dial(ctx, c.url, nil)
	if err != nil {
		return fmt.Errorf("dial clob websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(4 << 20)
	if err := c.subscribe(ctx, conn); err != nil {
		return err
	}
	c.connected.Store(true)
	for {
		select {
		case <-c.changed:
			return fmt.Errorf("market subscriptions changed")
		default:
		}
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if err := c.handle(ctx, raw); err != nil {
			return err
		}
	}
}

func (c *Client) subscribe(ctx context.Context, conn *websocket.Conn) error {
	c.mu.RLock()
	ids := make([]string, 0, len(c.tokens))
	for id := range c.tokens {
		ids = append(ids, id)
	}
	c.mu.RUnlock()
	if len(ids) == 0 {
		return fmt.Errorf("no clob tokens available")
	}
	return wsjson.Write(ctx, conn, map[string]any{"assets_ids": ids, "type": "market", "custom_feature_enabled": true})
}

type message struct {
	EventType      string `json:"event_type"`
	AssetID        string `json:"asset_id"`
	Market         string `json:"market"`
	BestBid        string `json:"best_bid"`
	BestAsk        string `json:"best_ask"`
	Spread         string `json:"spread"`
	Price          string `json:"price"`
	Side           string `json:"side"`
	Size           string `json:"size"`
	FeeRateBPS     string `json:"fee_rate_bps"`
	Timestamp      string `json:"timestamp"`
	WinningAssetID string `json:"winning_asset_id"`
	WinningOutcome string `json:"winning_outcome"`
}

func (c *Client) handle(ctx context.Context, raw []byte) error {
	if len(raw) == 0 || string(raw) == "PONG" {
		return nil
	}
	c.lastMessage.Store(c.now().UnixMilli())
	var batch []message
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &batch); err != nil {
			return err
		}
	} else {
		var item message
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		batch = []message{item}
	}
	for _, item := range batch {
		eventTime, err := strconv.ParseInt(item.Timestamp, 10, 64)
		if err != nil {
			return fmt.Errorf("parse clob timestamp %q: %w", item.Timestamp, err)
		}
		received := c.now().UnixMilli()
		c.mu.RLock()
		info, ok := c.tokens[item.AssetID]
		c.mu.RUnlock()
		switch item.EventType {
		case "best_bid_ask":
			if ok {
				if err := c.sink.WriteBookTick(ctx, model.BookTick{MarketID: info.MarketID, TokenID: item.AssetID, Outcome: info.Outcome, BestBid: item.BestBid, BestAsk: item.BestAsk, Spread: item.Spread, EventTimeMS: eventTime, ReceivedAtMS: received}); err != nil {
					return err
				}
			}
		case "last_trade_price":
			if ok {
				if err := c.sink.WriteTrade(ctx, model.Trade{MarketID: info.MarketID, TokenID: item.AssetID, Outcome: info.Outcome, Side: item.Side, Price: item.Price, Size: item.Size, FeeRateBPS: item.FeeRateBPS, EventTimeMS: eventTime, ReceivedAtMS: received}); err != nil {
					return err
				}
			}
		case "market_resolved":
			c.mu.RLock()
			winner, found := c.tokens[item.WinningAssetID]
			c.mu.RUnlock()
			if !found {
				return fmt.Errorf("resolved token %q is not subscribed", item.WinningAssetID)
			}
			if err := c.sink.WriteResolution(ctx, model.Resolution{MarketID: winner.MarketID, WinningTokenID: item.WinningAssetID, WinningOutcome: winner.Outcome, EventTimeMS: eventTime}); err != nil {
				return err
			}
		}
	}
	return nil
}
func (c *Client) Stats() (bool, int64, int64) {
	return c.connected.Load(), c.lastMessage.Load(), c.reconnects.Load()
}

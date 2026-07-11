package rtds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"alphaflow/go-service/polymarket-research/internal/model"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type Sink interface {
	WriteReferencePrice(context.Context, model.ReferencePrice) error
}
type Client struct {
	url         string
	sink        Sink
	reconnect   time.Duration
	now         func() time.Time
	connected   atomic.Bool
	lastMessage atomic.Int64
	reconnects  atomic.Int64
}

func New(url string, sink Sink, reconnect time.Duration) *Client {
	return &Client{url: url, sink: sink, reconnect: reconnect, now: time.Now}
}
func (c *Client) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := c.runOnce(ctx); err != nil && ctx.Err() == nil {
			c.connected.Store(false)
			c.reconnects.Add(1)
			slog.Warn("rtds websocket disconnected", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(c.reconnect):
			}
		}
	}
}
func (c *Client) runOnce(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, c.url, nil)
	if err != nil {
		return fmt.Errorf("dial rtds websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(4 << 20)
	subs := map[string]any{"action": "subscribe", "subscriptions": []map[string]any{{"topic": "crypto_prices", "type": "update", "filters": "btcusdt,ethusdt,solusdt,xrpusdt"}, {"topic": "crypto_prices_chainlink", "type": "*", "filters": ""}}}
	if err := wsjson.Write(ctx, conn, subs); err != nil {
		return err
	}
	c.connected.Store(true)
	ping := time.NewTicker(5 * time.Second)
	defer ping.Stop()
	errCh := make(chan error, 1)
	go func() {
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				errCh <- err
				return
			}
			if err := c.handle(ctx, raw); err != nil {
				errCh <- err
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errCh:
			return err
		case <-ping.C:
			if err := conn.Write(ctx, websocket.MessageText, []byte("PING")); err != nil {
				return err
			}
		}
	}
}

type message struct {
	Topic, Type string
	Timestamp   int64
	Payload     struct {
		Symbol    string      `json:"symbol"`
		Timestamp int64       `json:"timestamp"`
		Value     json.Number `json:"value"`
	} `json:"payload"`
}

func (c *Client) handle(ctx context.Context, raw []byte) error {
	if len(raw) == 0 || string(raw) == "PONG" {
		return nil
	}
	c.lastMessage.Store(c.now().UnixMilli())
	var item message
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	if item.Payload.Symbol == "" || item.Payload.Value == "" {
		return nil
	}
	symbol := normalize(item.Payload.Symbol)
	if symbol == "" {
		return nil
	}
	source := "binance"
	if item.Topic == "crypto_prices_chainlink" {
		source = "chainlink"
	}
	return c.sink.WriteReferencePrice(ctx, model.ReferencePrice{Source: source, Symbol: symbol, Price: item.Payload.Value.String(), EventTimeMS: item.Payload.Timestamp, ReceivedAtMS: c.now().UnixMilli()})
}
func (c *Client) Stats() (bool, int64, int64) {
	return c.connected.Load(), c.lastMessage.Load(), c.reconnects.Load()
}
func normalize(value string) string {
	value = strings.ToLower(value)
	pairs := map[string]string{"btcusdt": "BTC", "btc/usd": "BTC", "ethusdt": "ETH", "eth/usd": "ETH", "solusdt": "SOL", "sol/usd": "SOL", "xrpusdt": "XRP", "xrp/usd": "XRP"}
	return pairs[value]
}

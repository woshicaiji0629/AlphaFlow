package okx

import (
	"context"
	"encoding/json"
	"fmt"

	"alphaflow/go-service/market-data/internal/exchange"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type WSClient struct {
	baseURL string
}

type subscribeMessage struct {
	Op   string    `json:"op"`
	Args []channel `json:"args"`
}

type channel struct {
	Channel string `json:"channel"`
	InstID  string `json:"instId"`
}

type message struct {
	Event string     `json:"event"`
	Arg   channel    `json:"arg"`
	Data  [][]string `json:"data"`
}

func NewWSClient(baseURL string) *WSClient {
	return &WSClient{baseURL: baseURL}
}

func (c *WSClient) Run(
	ctx context.Context,
	streams []exchange.Stream,
	handler exchange.Handler,
) error {
	conn, _, err := websocket.Dial(ctx, c.baseURL, nil)
	if err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "closing")

	args := make([]channel, 0, len(streams))
	for _, stream := range streams {
		if stream.Type != exchange.StreamTypeKline {
			continue
		}
		args = append(args, channel{
			Channel: "candle" + okxInterval(stream.Interval),
			InstID:  stream.Symbol,
		})
	}
	if len(args) == 0 {
		<-ctx.Done()
		return nil
	}

	if err := wsjson.Write(ctx, conn, subscribeMessage{Op: "subscribe", Args: args}); err != nil {
		return fmt.Errorf("subscribe websocket: %w", err)
	}

	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, conn, &raw); err != nil {
			return fmt.Errorf("read websocket: %w", err)
		}

		if err := dispatch(ctx, raw, handler); err != nil {
			return err
		}
	}
}

func dispatch(ctx context.Context, raw json.RawMessage, handler exchange.Handler) error {
	var msg message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("decode websocket message: %w", err)
	}
	if msg.Event != "" || len(msg.Data) == 0 {
		return nil
	}

	interval, ok := intervalFromChannel(msg.Arg.Channel)
	if !ok {
		return nil
	}

	for _, item := range msg.Data {
		kline, err := parseKline(msg.Arg.InstID, interval, item)
		if err != nil {
			return err
		}
		if err := handler.HandleKline(ctx, kline); err != nil {
			return err
		}
	}
	return nil
}

func intervalFromChannel(channel string) (string, bool) {
	if len(channel) <= len("candle") {
		return "", false
	}
	value := channel[len("candle"):]
	switch value {
	case "1H":
		return "1h", true
	case "2H":
		return "2h", true
	case "4H":
		return "4h", true
	default:
		return value, true
	}
}

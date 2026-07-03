package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/market-data/internal/exchange"
	"alphaflow/go-service/market-data/internal/model"
	exchangebitget "alphaflow/go-service/pkg/exchangeclient/bitget"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type WSClient struct {
	baseURL     string
	productType string
}

type subscribeMessage struct {
	Op   string    `json:"op"`
	Args []channel `json:"args"`
}

type channel struct {
	InstType string `json:"instType"`
	Channel  string `json:"channel"`
	InstID   string `json:"instId"`
}

type message struct {
	Action string          `json:"action"`
	Arg    channel         `json:"arg"`
	Data   json.RawMessage `json:"data"`
	Ts     flexInt64       `json:"ts"`
}

type wsTicker struct {
	InstID        string    `json:"instId"`
	LastPrice     any       `json:"lastPr"`
	MarkPrice     any       `json:"markPrice"`
	IndexPrice    any       `json:"indexPrice"`
	FundingRate   any       `json:"fundingRate"`
	NextFunding   flexInt64 `json:"nextFundingTime"`
	BidPrice      any       `json:"bidPr"`
	BidQuantity   any       `json:"bidSz"`
	AskPrice      any       `json:"askPr"`
	AskQuantity   any       `json:"askSz"`
	OpenInterest  any       `json:"holdingAmount"`
	OpenInterest2 any       `json:"openInterest"`
	Ts            flexInt64 `json:"ts"`
}

type wsTrade struct {
	InstID  string    `json:"instId"`
	TradeID any       `json:"tradeId"`
	Price   any       `json:"price"`
	Size    any       `json:"size"`
	Ts      flexInt64 `json:"ts"`
}

func NewWSClient(baseURL string, productType string) *WSClient {
	return &WSClient{baseURL: baseURL, productType: productType}
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
	conn.SetReadLimit(exchange.WebSocketReadLimit)
	defer conn.Close(websocket.StatusNormalClosure, "closing")

	args := make([]channel, 0, len(streams))
	seen := map[string]struct{}{}
	for _, stream := range streams {
		switch stream.Type {
		case exchange.StreamTypeKline:
			args = appendChannel(args, seen, channel{
				InstType: c.productType,
				Channel:  "candle" + exchangebitget.Interval(stream.Interval),
				InstID:   stream.Symbol,
			})
		case exchange.StreamTypeAggTrade:
			args = appendChannel(args, seen, channel{
				InstType: c.productType,
				Channel:  "trade",
				InstID:   stream.Symbol,
			})
		case exchange.StreamTypeMarkPrice, exchange.StreamTypeBookTicker:
			args = appendChannel(args, seen, channel{
				InstType: c.productType,
				Channel:  "ticker",
				InstID:   stream.Symbol,
			})
		}
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
		if err := c.dispatch(ctx, raw, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			exchange.LogWebSocketDispatchError("bitget", raw, err)
			continue
		}
	}
}

func appendChannel(args []channel, seen map[string]struct{}, item channel) []channel {
	key := item.InstType + ":" + item.Channel + ":" + item.InstID
	if _, ok := seen[key]; ok {
		return args
	}
	seen[key] = struct{}{}
	return append(args, item)
}

func (c *WSClient) dispatch(ctx context.Context, raw json.RawMessage, handler exchange.Handler) error {
	var msg message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("decode websocket message: %w", err)
	}
	if len(msg.Data) == 0 {
		return nil
	}

	switch {
	case strings.HasPrefix(msg.Arg.Channel, "candle"):
		return c.dispatchKlines(ctx, msg, handler)
	case msg.Arg.Channel == "ticker":
		return c.dispatchTickers(ctx, msg, handler)
	case msg.Arg.Channel == "trade":
		return c.dispatchTrades(ctx, msg, handler)
	default:
		return nil
	}
}

func (c *WSClient) dispatchKlines(ctx context.Context, msg message, handler exchange.Handler) error {
	interval, ok := intervalFromChannel(msg.Arg.Channel)
	if !ok {
		return nil
	}
	var data [][]string
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return fmt.Errorf("decode kline data: %w", err)
	}

	for _, item := range data {
		kline, err := exchangebitget.KlineFromRaw(c.productType, msg.Arg.InstID, interval, item)
		if err != nil {
			return err
		}
		if err := handler.HandleKline(ctx, kline); err != nil {
			return err
		}
	}
	return nil
}

func (c *WSClient) dispatchTickers(ctx context.Context, msg message, handler exchange.Handler) error {
	var data []wsTicker
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return fmt.Errorf("decode ticker data: %w", err)
	}
	for _, item := range data {
		eventTime := int64(item.Ts)
		if eventTime == 0 {
			eventTime = int64(msg.Ts)
		}
		if stringify(item.LastPrice) != "" {
			if err := handler.HandleLastPrice(ctx, c.lastPriceFromTicker(eventTime, item)); err != nil {
				return err
			}
		}
		if stringify(item.MarkPrice) != "" {
			if err := handler.HandleMarkPrice(ctx, c.markPriceFromTicker(eventTime, item)); err != nil {
				return err
			}
		}
		if stringify(item.BidPrice) != "" || stringify(item.AskPrice) != "" {
			if err := handler.HandleBookTicker(ctx, c.bookTickerFromTicker(eventTime, item)); err != nil {
				return err
			}
		}
		openInterest := stringify(item.OpenInterest)
		if openInterest == "" {
			openInterest = stringify(item.OpenInterest2)
		}
		if openInterest != "" {
			if err := handler.HandleOpenInterest(ctx, c.openInterestFromTicker(eventTime, item, openInterest)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *WSClient) dispatchTrades(ctx context.Context, msg message, handler exchange.Handler) error {
	var data []wsTrade
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return fmt.Errorf("decode trade data: %w", err)
	}
	for _, item := range data {
		eventTime := int64(item.Ts)
		if eventTime == 0 {
			eventTime = int64(msg.Ts)
		}
		if err := handler.HandleLastPrice(ctx, c.lastPriceFromTrade(eventTime, msg.Arg.InstID, item)); err != nil {
			return err
		}
	}
	return nil
}

func (c *WSClient) lastPriceFromTicker(eventTime int64, raw wsTicker) model.LastPrice {
	return model.LastPrice{
		Exchange:  "bitget",
		Market:    strings.ToLower(c.productType),
		Symbol:    raw.InstID,
		Price:     stringify(raw.LastPrice),
		EventTime: eventTime,
		TradeTime: eventTime,
	}
}

func (c *WSClient) lastPriceFromTrade(eventTime int64, symbol string, raw wsTrade) model.LastPrice {
	if raw.InstID != "" {
		symbol = raw.InstID
	}
	return model.LastPrice{
		Exchange:  "bitget",
		Market:    strings.ToLower(c.productType),
		Symbol:    symbol,
		Price:     stringify(raw.Price),
		Quantity:  stringify(raw.Size),
		EventTime: eventTime,
		TradeTime: eventTime,
		TradeID:   int64ify(raw.TradeID),
	}
}

func (c *WSClient) markPriceFromTicker(eventTime int64, raw wsTicker) model.MarkPrice {
	return model.MarkPrice{
		Exchange:        "bitget",
		Market:          strings.ToLower(c.productType),
		Symbol:          raw.InstID,
		MarkPrice:       stringify(raw.MarkPrice),
		IndexPrice:      stringify(raw.IndexPrice),
		FundingRate:     stringify(raw.FundingRate),
		NextFundingTime: int64(raw.NextFunding),
		EventTime:       eventTime,
	}
}

func (c *WSClient) bookTickerFromTicker(eventTime int64, raw wsTicker) model.BookTicker {
	return model.BookTicker{
		Exchange:        "bitget",
		Market:          strings.ToLower(c.productType),
		Symbol:          raw.InstID,
		BidPrice:        stringify(raw.BidPrice),
		BidQuantity:     stringify(raw.BidQuantity),
		AskPrice:        stringify(raw.AskPrice),
		AskQuantity:     stringify(raw.AskQuantity),
		EventTime:       eventTime,
		TransactionTime: eventTime,
	}
}

func (c *WSClient) openInterestFromTicker(eventTime int64, raw wsTicker, openInterest string) model.OpenInterest {
	return model.OpenInterest{
		Exchange:     "bitget",
		Market:       strings.ToLower(c.productType),
		Symbol:       raw.InstID,
		OpenInterest: openInterest,
		Time:         eventTime,
	}
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

func stringify(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

func int64ify(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

type flexInt64 int64

func (v *flexInt64) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		if text == "" {
			*v = 0
			return nil
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return err
		}
		*v = flexInt64(parsed)
		return nil
	}

	var number float64
	if err := json.Unmarshal(data, &number); err == nil {
		*v = flexInt64(number)
		return nil
	}

	return fmt.Errorf("invalid int64 value: %s", string(data))
}

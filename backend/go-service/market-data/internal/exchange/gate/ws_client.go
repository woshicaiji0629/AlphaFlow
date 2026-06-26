package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"alphaflow/go-service/market-data/internal/exchange"
	"alphaflow/go-service/market-data/internal/model"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type WSClient struct {
	baseURL       string
	settle        string
	statsInterval string
}

type subscribeMessage struct {
	Time    int64    `json:"time"`
	Channel string   `json:"channel"`
	Event   string   `json:"event"`
	Payload []string `json:"payload"`
}

type messageEnvelope struct {
	TimeMS  int64           `json:"time_ms"`
	Channel string          `json:"channel"`
	Event   string          `json:"event"`
	Result  json.RawMessage `json:"result"`
}

type wsKline struct {
	Time        int64  `json:"t"`
	Volume      string `json:"v"`
	Close       string `json:"c"`
	High        string `json:"h"`
	Low         string `json:"l"`
	Open        string `json:"o"`
	Name        string `json:"n"`
	QuoteVolume string `json:"a"`
	IsClosed    bool   `json:"w"`
}

type wsTrade struct {
	Size         any    `json:"size"`
	ID           int64  `json:"id"`
	CreateTimeMS int64  `json:"create_time_ms"`
	Price        string `json:"price"`
	Contract     string `json:"contract"`
}

type wsBookTicker struct {
	TimeMS      int64  `json:"t"`
	UpdateID    any    `json:"u"`
	Contract    string `json:"s"`
	BidQuantity any    `json:"B"`
	BidPrice    string `json:"b"`
	AskQuantity any    `json:"A"`
	AskPrice    string `json:"a"`
}

type wsContractStat struct {
	Time         int64  `json:"time"`
	Contract     string `json:"contract"`
	MarkPrice    any    `json:"mark_price"`
	OpenInterest any    `json:"open_interest"`
}

type wsLiquidation struct {
	Price    any    `json:"price"`
	Size     any    `json:"size"`
	TimeMS   int64  `json:"time_ms"`
	Contract string `json:"contract"`
}

func NewWSClient(baseURL string, settle string, statsInterval string) *WSClient {
	return &WSClient{baseURL: baseURL, settle: settle, statsInterval: statsInterval}
}

func (c *WSClient) Run(
	ctx context.Context,
	streams []exchange.Stream,
	handler exchange.Handler,
) error {
	conn, _, err := websocket.Dial(ctx, c.baseURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-Gate-Size-Decimal": []string{"1"}},
	})
	if err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}
	conn.SetReadLimit(exchange.WebSocketReadLimit)
	defer conn.Close(websocket.StatusNormalClosure, "closing")

	for _, stream := range streams {
		requests := c.subscribeMessages(stream)
		for _, request := range requests {
			if err := wsjson.Write(ctx, conn, request); err != nil {
				return fmt.Errorf("subscribe websocket: %w", err)
			}
		}
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
			exchange.LogWebSocketDispatchError("gate", raw, err)
			continue
		}
	}
}

func (c *WSClient) dispatch(
	ctx context.Context,
	raw json.RawMessage,
	handler exchange.Handler,
) error {
	var msg messageEnvelope
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("decode websocket message: %w", err)
	}
	if msg.Event != "update" {
		return nil
	}

	switch msg.Channel {
	case "futures.candlesticks":
		return c.dispatchKlines(ctx, msg, handler)
	case "futures.trades":
		return c.dispatchTrades(ctx, msg, handler)
	case "futures.book_ticker":
		return c.dispatchBookTickers(ctx, msg, handler)
	case "futures.contract_stats":
		return c.dispatchContractStats(ctx, msg, handler)
	case "futures.public_liquidates":
		return c.dispatchLiquidations(ctx, msg, handler)
	default:
		return nil
	}
}

func (c *WSClient) subscribeMessages(stream exchange.Stream) []subscribeMessage {
	now := time.Now().Unix()
	switch stream.Type {
	case exchange.StreamTypeKline:
		return []subscribeMessage{{
			Time:    now,
			Channel: "futures.candlesticks",
			Event:   "subscribe",
			Payload: []string{stream.Interval, stream.Symbol},
		}}
	case exchange.StreamTypeAggTrade:
		return []subscribeMessage{{
			Time:    now,
			Channel: "futures.trades",
			Event:   "subscribe",
			Payload: []string{stream.Symbol},
		}}
	case exchange.StreamTypeBookTicker:
		return []subscribeMessage{{
			Time:    now,
			Channel: "futures.book_ticker",
			Event:   "subscribe",
			Payload: []string{stream.Symbol},
		}}
	case exchange.StreamTypeMarkPrice:
		return []subscribeMessage{{
			Time:    now,
			Channel: "futures.contract_stats",
			Event:   "subscribe",
			Payload: []string{stream.Symbol, c.statsInterval},
		}}
	case exchange.StreamTypeForceOrder:
		return []subscribeMessage{{
			Time:    now,
			Channel: "futures.public_liquidates",
			Event:   "subscribe",
			Payload: []string{stream.Symbol},
		}}
	default:
		return nil
	}
}

func (c *WSClient) dispatchKlines(
	ctx context.Context,
	msg messageEnvelope,
	handler exchange.Handler,
) error {
	var result []wsKline
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		return fmt.Errorf("decode kline result: %w", err)
	}
	for _, item := range result {
		kline, err := c.klineFromWS(msg.TimeMS, item)
		if err != nil {
			return err
		}
		if err := handler.HandleKline(ctx, kline); err != nil {
			return err
		}
	}
	return nil
}

func (c *WSClient) dispatchTrades(
	ctx context.Context,
	msg messageEnvelope,
	handler exchange.Handler,
) error {
	var result []wsTrade
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		return fmt.Errorf("decode trade result: %w", err)
	}
	for _, item := range result {
		if err := handler.HandleLastPrice(ctx, c.lastPriceFromWS(msg.TimeMS, item)); err != nil {
			return err
		}
	}
	return nil
}

func (c *WSClient) dispatchBookTickers(
	ctx context.Context,
	msg messageEnvelope,
	handler exchange.Handler,
) error {
	var result wsBookTicker
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		return fmt.Errorf("decode book ticker result: %w", err)
	}
	if err := handler.HandleBookTicker(ctx, c.bookTickerFromWS(msg.TimeMS, result)); err != nil {
		return err
	}
	return nil
}

func (c *WSClient) dispatchContractStats(
	ctx context.Context,
	msg messageEnvelope,
	handler exchange.Handler,
) error {
	var result []wsContractStat
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		return fmt.Errorf("decode contract stats result: %w", err)
	}
	for _, item := range result {
		if stringify(item.MarkPrice) != "" {
			if err := handler.HandleMarkPrice(ctx, c.markPriceFromWS(msg.TimeMS, item)); err != nil {
				return err
			}
		}
		if stringify(item.OpenInterest) != "" {
			if err := handler.HandleOpenInterest(ctx, c.openInterestFromWS(item)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *WSClient) dispatchLiquidations(
	ctx context.Context,
	msg messageEnvelope,
	handler exchange.Handler,
) error {
	result, err := decodeLiquidations(msg.Result)
	if err != nil {
		return fmt.Errorf("decode liquidation result: %w", err)
	}
	for _, item := range result {
		if err := handler.HandleLiquidation(ctx, c.liquidationFromWS(msg.TimeMS, item)); err != nil {
			return err
		}
	}
	return nil
}

func decodeLiquidations(raw json.RawMessage) ([]wsLiquidation, error) {
	var result []wsLiquidation
	if err := json.Unmarshal(raw, &result); err == nil {
		return result, nil
	}

	var item wsLiquidation
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}
	return []wsLiquidation{item}, nil
}

func (c *WSClient) klineFromWS(eventTime int64, raw wsKline) (model.Kline, error) {
	interval, symbol, err := parseStreamName(raw.Name)
	if err != nil {
		return model.Kline{}, err
	}
	openTime := raw.Time * 1000
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return model.Kline{}, err
	}

	return model.Kline{
		Exchange:    "gate",
		Market:      c.settle,
		Symbol:      symbol,
		Interval:    interval,
		OpenTime:    openTime,
		CloseTime:   openTime + intervalMillis - 1,
		Open:        raw.Open,
		High:        raw.High,
		Low:         raw.Low,
		Close:       raw.Close,
		Volume:      raw.Volume,
		QuoteVolume: raw.QuoteVolume,
		IsClosed:    raw.IsClosed,
		EventTime:   eventTime,
	}, nil
}

func (c *WSClient) lastPriceFromWS(eventTime int64, raw wsTrade) model.LastPrice {
	return model.LastPrice{
		Exchange:  "gate",
		Market:    c.settle,
		Symbol:    raw.Contract,
		Price:     raw.Price,
		Quantity:  stringify(raw.Size),
		EventTime: eventTime,
		TradeTime: raw.CreateTimeMS,
		TradeID:   raw.ID,
	}
}

func (c *WSClient) bookTickerFromWS(eventTime int64, raw wsBookTicker) model.BookTicker {
	return model.BookTicker{
		Exchange:        "gate",
		Market:          c.settle,
		Symbol:          raw.Contract,
		UpdateID:        int64ify(raw.UpdateID),
		BidPrice:        raw.BidPrice,
		BidQuantity:     stringify(raw.BidQuantity),
		AskPrice:        raw.AskPrice,
		AskQuantity:     stringify(raw.AskQuantity),
		EventTime:       eventTime,
		TransactionTime: raw.TimeMS,
	}
}

func (c *WSClient) markPriceFromWS(eventTime int64, raw wsContractStat) model.MarkPrice {
	return model.MarkPrice{
		Exchange:  "gate",
		Market:    c.settle,
		Symbol:    raw.Contract,
		MarkPrice: stringify(raw.MarkPrice),
		EventTime: eventTime,
	}
}

func (c *WSClient) openInterestFromWS(raw wsContractStat) model.OpenInterest {
	return model.OpenInterest{
		Exchange:     "gate",
		Market:       c.settle,
		Symbol:       raw.Contract,
		OpenInterest: stringify(raw.OpenInterest),
		Time:         raw.Time * 1000,
	}
}

func (c *WSClient) liquidationFromWS(eventTime int64, raw wsLiquidation) model.Liquidation {
	return model.Liquidation{
		Exchange:         "gate",
		Market:           c.settle,
		Symbol:           raw.Contract,
		OriginalQuantity: stringify(raw.Size),
		Price:            stringify(raw.Price),
		LastFilledQty:    stringify(raw.Size),
		AccumulatedQty:   stringify(raw.Size),
		TradeTime:        raw.TimeMS,
		EventTime:        eventTime,
	}
}

func parseStreamName(name string) (string, string, error) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid gate stream name %q", name)
	}
	return parts[0], parts[1], nil
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

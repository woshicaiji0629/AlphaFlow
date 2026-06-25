package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"alphaflow/go-service/market-data/internal/exchange"
	"alphaflow/go-service/market-data/internal/model"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type WSClient struct {
	baseURL string
}

type combinedMessage struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

type wsKlineMessage struct {
	EventType string  `json:"e"`
	EventTime int64   `json:"E"`
	Symbol    string  `json:"s"`
	Kline     wsKline `json:"k"`
}

type wsAggTradeMessage struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`
	TradeID   int64  `json:"a"`
	Price     string `json:"p"`
	Quantity  string `json:"q"`
	TradeTime int64  `json:"T"`
}

type wsMarkPriceMessage struct {
	EventType       string `json:"e"`
	EventTime       int64  `json:"E"`
	Symbol          string `json:"s"`
	MarkPrice       string `json:"p"`
	IndexPrice      string `json:"i"`
	FundingRate     string `json:"r"`
	NextFundingTime int64  `json:"T"`
}

type wsBookTickerMessage struct {
	EventType       string `json:"e"`
	UpdateID        int64  `json:"u"`
	Symbol          string `json:"s"`
	EventTime       int64  `json:"E"`
	TransactionTime int64  `json:"T"`
	BidPrice        string `json:"b"`
	BidQuantity     string `json:"B"`
	AskPrice        string `json:"a"`
	AskQuantity     string `json:"A"`
}

type wsForceOrderMessage struct {
	EventType string       `json:"e"`
	EventTime int64        `json:"E"`
	Order     wsForceOrder `json:"o"`
}

type wsForceOrder struct {
	Symbol           string `json:"s"`
	Side             string `json:"S"`
	OrderType        string `json:"o"`
	TimeInForce      string `json:"f"`
	OriginalQuantity string `json:"q"`
	Price            string `json:"p"`
	AveragePrice     string `json:"ap"`
	OrderStatus      string `json:"X"`
	LastFilledQty    string `json:"l"`
	AccumulatedQty   string `json:"z"`
	TradeTime        int64  `json:"T"`
}

type wsKline struct {
	OpenTime            int64  `json:"t"`
	CloseTime           int64  `json:"T"`
	Symbol              string `json:"s"`
	Interval            string `json:"i"`
	FirstTradeID        int64  `json:"f"`
	LastTradeID         int64  `json:"L"`
	Open                string `json:"o"`
	Close               string `json:"c"`
	High                string `json:"h"`
	Low                 string `json:"l"`
	Volume              string `json:"v"`
	TradeCount          int64  `json:"n"`
	IsClosed            bool   `json:"x"`
	QuoteVolume         string `json:"q"`
	TakerBuyVolume      string `json:"V"`
	TakerBuyQuoteVolume string `json:"Q"`
	Ignore              string `json:"B"`
}

func NewWSClient(baseURL string) *WSClient {
	return &WSClient{baseURL: baseURL}
}

func (c *WSClient) Run(
	ctx context.Context,
	streams []exchange.Stream,
	handler exchange.Handler,
) error {
	conn, _, err := websocket.Dial(ctx, c.streamURL(streams), nil)
	if err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "closing")

	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, conn, &raw); err != nil {
			return fmt.Errorf("read websocket: %w", err)
		}

		if err := c.dispatch(ctx, raw, handler); err != nil {
			return err
		}
	}
}

func (c *WSClient) dispatch(
	ctx context.Context,
	raw json.RawMessage,
	handler exchange.Handler,
) error {
	var envelope combinedMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode websocket envelope: %w", err)
	}

	eventType, err := eventType(envelope.Data)
	if err != nil {
		return err
	}

	switch eventType {
	case "kline":
		var message combinedKlineMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			return fmt.Errorf("decode kline: %w", err)
		}
		return handler.HandleKline(ctx, klineFromWS(message.Data))
	case "aggTrade":
		var message combinedAggTradeMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			return fmt.Errorf("decode agg trade: %w", err)
		}
		return handler.HandleLastPrice(ctx, lastPriceFromWS(message.Data))
	case "markPriceUpdate":
		var message combinedMarkPriceMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			return fmt.Errorf("decode mark price: %w", err)
		}
		return handler.HandleMarkPrice(ctx, markPriceFromWS(message.Data))
	case "bookTicker":
		var message combinedBookTickerMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			return fmt.Errorf("decode book ticker: %w", err)
		}
		return handler.HandleBookTicker(ctx, bookTickerFromWS(message.Data))
	case "forceOrder":
		var message combinedForceOrderMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			return fmt.Errorf("decode force order: %w", err)
		}
		return handler.HandleLiquidation(ctx, liquidationFromWS(message.Data))
	default:
		return nil
	}
}

func eventType(raw json.RawMessage) (string, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", fmt.Errorf("decode event fields: %w", err)
	}

	value, ok := fields["e"]
	if !ok {
		return "", nil
	}

	var eventType string
	if err := json.Unmarshal(value, &eventType); err != nil {
		return "", fmt.Errorf("decode event type: %w", err)
	}
	return eventType, nil
}

type combinedKlineMessage struct {
	Stream string         `json:"stream"`
	Data   wsKlineMessage `json:"data"`
}

type combinedAggTradeMessage struct {
	Stream string            `json:"stream"`
	Data   wsAggTradeMessage `json:"data"`
}

type combinedMarkPriceMessage struct {
	Stream string             `json:"stream"`
	Data   wsMarkPriceMessage `json:"data"`
}

type combinedBookTickerMessage struct {
	Stream string              `json:"stream"`
	Data   wsBookTickerMessage `json:"data"`
}

type combinedForceOrderMessage struct {
	Stream string              `json:"stream"`
	Data   wsForceOrderMessage `json:"data"`
}

func klineFromWS(message wsKlineMessage) model.Kline {
	return model.Kline{
		Exchange:            "binance",
		Market:              "um",
		Symbol:              message.Symbol,
		Interval:            message.Kline.Interval,
		OpenTime:            message.Kline.OpenTime,
		CloseTime:           message.Kline.CloseTime,
		Open:                message.Kline.Open,
		High:                message.Kline.High,
		Low:                 message.Kline.Low,
		Close:               message.Kline.Close,
		Volume:              message.Kline.Volume,
		QuoteVolume:         message.Kline.QuoteVolume,
		TradeCount:          message.Kline.TradeCount,
		TakerBuyVolume:      message.Kline.TakerBuyVolume,
		TakerBuyQuoteVolume: message.Kline.TakerBuyQuoteVolume,
		IsClosed:            message.Kline.IsClosed,
		EventTime:           message.EventTime,
		FirstTradeID:        message.Kline.FirstTradeID,
		LastTradeID:         message.Kline.LastTradeID,
	}
}

func lastPriceFromWS(message wsAggTradeMessage) model.LastPrice {
	return model.LastPrice{
		Exchange:  "binance",
		Market:    "um",
		Symbol:    message.Symbol,
		Price:     message.Price,
		Quantity:  message.Quantity,
		EventTime: message.EventTime,
		TradeTime: message.TradeTime,
		TradeID:   message.TradeID,
	}
}

func markPriceFromWS(message wsMarkPriceMessage) model.MarkPrice {
	return model.MarkPrice{
		Exchange:        "binance",
		Market:          "um",
		Symbol:          message.Symbol,
		MarkPrice:       message.MarkPrice,
		IndexPrice:      message.IndexPrice,
		FundingRate:     message.FundingRate,
		NextFundingTime: message.NextFundingTime,
		EventTime:       message.EventTime,
	}
}

func bookTickerFromWS(message wsBookTickerMessage) model.BookTicker {
	return model.BookTicker{
		Exchange:        "binance",
		Market:          "um",
		Symbol:          message.Symbol,
		UpdateID:        message.UpdateID,
		BidPrice:        message.BidPrice,
		BidQuantity:     message.BidQuantity,
		AskPrice:        message.AskPrice,
		AskQuantity:     message.AskQuantity,
		EventTime:       message.EventTime,
		TransactionTime: message.TransactionTime,
	}
}

func liquidationFromWS(message wsForceOrderMessage) model.Liquidation {
	return model.Liquidation{
		Exchange:         "binance",
		Market:           "um",
		Symbol:           message.Order.Symbol,
		Side:             message.Order.Side,
		OrderType:        message.Order.OrderType,
		TimeInForce:      message.Order.TimeInForce,
		OriginalQuantity: message.Order.OriginalQuantity,
		Price:            message.Order.Price,
		AveragePrice:     message.Order.AveragePrice,
		OrderStatus:      message.Order.OrderStatus,
		LastFilledQty:    message.Order.LastFilledQty,
		AccumulatedQty:   message.Order.AccumulatedQty,
		TradeTime:        message.Order.TradeTime,
		EventTime:        message.EventTime,
	}
}

func (c *WSClient) streamURL(streams []exchange.Stream) string {
	names := make([]string, 0, len(streams))
	for _, stream := range streams {
		switch stream.Type {
		case exchange.StreamTypeKline:
			names = append(names, fmt.Sprintf(
				"%s@kline_%s",
				strings.ToLower(stream.Symbol),
				stream.Interval,
			))
		case exchange.StreamTypeAggTrade:
			names = append(names, fmt.Sprintf("%s@aggTrade", strings.ToLower(stream.Symbol)))
		case exchange.StreamTypeMarkPrice:
			interval := stream.Interval
			if interval == "" {
				interval = "1s"
			}
			names = append(names, fmt.Sprintf(
				"%s@markPrice@%s",
				strings.ToLower(stream.Symbol),
				interval,
			))
		case exchange.StreamTypeBookTicker:
			names = append(names, fmt.Sprintf("%s@bookTicker", strings.ToLower(stream.Symbol)))
		case exchange.StreamTypeForceOrder:
			names = append(names, fmt.Sprintf("%s@forceOrder", strings.ToLower(stream.Symbol)))
		}
	}

	endpoint, err := url.Parse(c.baseURL + "/market/stream")
	if err != nil {
		return c.baseURL
	}
	query := endpoint.Query()
	query.Set("streams", strings.Join(names, "/"))
	endpoint.RawQuery = query.Encode()

	return endpoint.String()
}

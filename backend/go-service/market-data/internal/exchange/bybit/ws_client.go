package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/market-data/internal/exchange"
	"alphaflow/go-service/market-data/internal/model"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type WSClient struct {
	baseURL  string
	category string
}

type subscribeMessage struct {
	Op   string   `json:"op"`
	Args []string `json:"args"`
}

type message struct {
	Topic string          `json:"topic"`
	Ts    int64           `json:"ts"`
	Data  json.RawMessage `json:"data"`
}

type wsKline struct {
	Start     int64  `json:"start"`
	End       int64  `json:"end"`
	Interval  string `json:"interval"`
	Open      string `json:"open"`
	Close     string `json:"close"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Volume    string `json:"volume"`
	Turnover  string `json:"turnover"`
	Confirm   bool   `json:"confirm"`
	Timestamp int64  `json:"timestamp"`
}

type wsTrade struct {
	TradeTime int64  `json:"T"`
	Symbol    string `json:"s"`
	Side      string `json:"S"`
	Size      string `json:"v"`
	Price     string `json:"p"`
	TradeID   string `json:"i"`
}

type wsTicker struct {
	Symbol          string `json:"symbol"`
	LastPrice       string `json:"lastPrice"`
	MarkPrice       string `json:"markPrice"`
	IndexPrice      string `json:"indexPrice"`
	OpenInterest    string `json:"openInterest"`
	BidPrice        string `json:"bid1Price"`
	BidQuantity     string `json:"bid1Size"`
	AskPrice        string `json:"ask1Price"`
	AskQuantity     string `json:"ask1Size"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
	CrossSeq        int64  `json:"cs"`
}

type wsLiquidation struct {
	TradeTime int64  `json:"T"`
	Symbol    string `json:"s"`
	Side      string `json:"S"`
	Size      string `json:"v"`
	Price     string `json:"p"`
}

func NewWSClient(baseURL string, category string) *WSClient {
	return &WSClient{baseURL: baseURL, category: category}
}

const readLimit = 1 << 20

func (c *WSClient) Run(
	ctx context.Context,
	streams []exchange.Stream,
	handler exchange.Handler,
) error {
	conn, _, err := websocket.Dial(ctx, c.baseURL, nil)
	if err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}
	conn.SetReadLimit(readLimit)
	defer conn.Close(websocket.StatusNormalClosure, "closing")

	args := make([]string, 0, len(streams))
	seen := map[string]struct{}{}
	for _, stream := range streams {
		switch stream.Type {
		case exchange.StreamTypeKline:
			args = appendTopic(args, seen, "kline."+bybitInterval(stream.Interval)+"."+stream.Symbol)
		case exchange.StreamTypeAggTrade:
			args = appendTopic(args, seen, "publicTrade."+stream.Symbol)
		case exchange.StreamTypeMarkPrice, exchange.StreamTypeBookTicker:
			args = appendTopic(args, seen, "tickers."+stream.Symbol)
		case exchange.StreamTypeForceOrder:
			args = appendTopic(args, seen, "allLiquidation."+stream.Symbol)
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
			return err
		}
	}
}

func appendTopic(args []string, seen map[string]struct{}, topic string) []string {
	if _, ok := seen[topic]; ok {
		return args
	}
	seen[topic] = struct{}{}
	return append(args, topic)
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
	case strings.HasPrefix(msg.Topic, "kline."):
		return c.dispatchKlines(ctx, msg, handler)
	case strings.HasPrefix(msg.Topic, "publicTrade."):
		return c.dispatchTrades(ctx, msg, handler)
	case strings.HasPrefix(msg.Topic, "tickers."):
		return c.dispatchTickers(ctx, msg, handler)
	case strings.HasPrefix(msg.Topic, "allLiquidation."):
		return c.dispatchLiquidations(ctx, msg, handler)
	default:
		return nil
	}
}

func (c *WSClient) dispatchKlines(ctx context.Context, msg message, handler exchange.Handler) error {
	symbol, interval, ok := parseTopic(msg.Topic)
	if !ok {
		return nil
	}
	var data []wsKline
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return fmt.Errorf("decode kline data: %w", err)
	}
	for _, item := range data {
		kline := c.klineFromWS(symbol, interval, item)
		if err := handler.HandleKline(ctx, kline); err != nil {
			return err
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
		if err := handler.HandleLastPrice(ctx, c.lastPriceFromTrade(msg.Ts, item)); err != nil {
			return err
		}
	}
	return nil
}

func (c *WSClient) dispatchTickers(ctx context.Context, msg message, handler exchange.Handler) error {
	var data wsTicker
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return fmt.Errorf("decode ticker data: %w", err)
	}
	eventTime := msg.Ts
	if data.LastPrice != "" {
		if err := handler.HandleLastPrice(ctx, c.lastPriceFromTicker(eventTime, data)); err != nil {
			return err
		}
	}
	if data.MarkPrice != "" {
		if err := handler.HandleMarkPrice(ctx, c.markPriceFromTicker(eventTime, data)); err != nil {
			return err
		}
	}
	if data.BidPrice != "" || data.AskPrice != "" {
		if err := handler.HandleBookTicker(ctx, c.bookTickerFromTicker(eventTime, data)); err != nil {
			return err
		}
	}
	if data.OpenInterest != "" {
		if err := handler.HandleOpenInterest(ctx, c.openInterestFromTicker(eventTime, data)); err != nil {
			return err
		}
	}
	return nil
}

func (c *WSClient) dispatchLiquidations(ctx context.Context, msg message, handler exchange.Handler) error {
	var data []wsLiquidation
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return fmt.Errorf("decode liquidation data: %w", err)
	}
	for _, item := range data {
		if err := handler.HandleLiquidation(ctx, c.liquidationFromWS(msg.Ts, item)); err != nil {
			return err
		}
	}
	return nil
}

func (c *WSClient) klineFromWS(symbol string, interval string, raw wsKline) model.Kline {
	return model.Kline{
		Exchange:    "bybit",
		Market:      c.category,
		Symbol:      symbol,
		Interval:    interval,
		OpenTime:    raw.Start,
		CloseTime:   raw.End,
		Open:        raw.Open,
		High:        raw.High,
		Low:         raw.Low,
		Close:       raw.Close,
		Volume:      raw.Volume,
		QuoteVolume: raw.Turnover,
		IsClosed:    raw.Confirm,
		EventTime:   raw.Timestamp,
	}
}

func (c *WSClient) lastPriceFromTrade(eventTime int64, raw wsTrade) model.LastPrice {
	if eventTime == 0 {
		eventTime = raw.TradeTime
	}
	return model.LastPrice{
		Exchange:  "bybit",
		Market:    c.category,
		Symbol:    raw.Symbol,
		Price:     raw.Price,
		Quantity:  raw.Size,
		EventTime: eventTime,
		TradeTime: raw.TradeTime,
		TradeID:   int64ify(raw.TradeID),
	}
}

func (c *WSClient) lastPriceFromTicker(eventTime int64, raw wsTicker) model.LastPrice {
	return model.LastPrice{
		Exchange:  "bybit",
		Market:    c.category,
		Symbol:    raw.Symbol,
		Price:     raw.LastPrice,
		EventTime: eventTime,
		TradeTime: eventTime,
	}
}

func (c *WSClient) markPriceFromTicker(eventTime int64, raw wsTicker) model.MarkPrice {
	return model.MarkPrice{
		Exchange:        "bybit",
		Market:          c.category,
		Symbol:          raw.Symbol,
		MarkPrice:       raw.MarkPrice,
		IndexPrice:      raw.IndexPrice,
		FundingRate:     raw.FundingRate,
		NextFundingTime: int64ify(raw.NextFundingTime),
		EventTime:       eventTime,
	}
}

func (c *WSClient) bookTickerFromTicker(eventTime int64, raw wsTicker) model.BookTicker {
	return model.BookTicker{
		Exchange:        "bybit",
		Market:          c.category,
		Symbol:          raw.Symbol,
		UpdateID:        raw.CrossSeq,
		BidPrice:        raw.BidPrice,
		BidQuantity:     raw.BidQuantity,
		AskPrice:        raw.AskPrice,
		AskQuantity:     raw.AskQuantity,
		EventTime:       eventTime,
		TransactionTime: eventTime,
	}
}

func (c *WSClient) openInterestFromTicker(eventTime int64, raw wsTicker) model.OpenInterest {
	return model.OpenInterest{
		Exchange:     "bybit",
		Market:       c.category,
		Symbol:       raw.Symbol,
		OpenInterest: raw.OpenInterest,
		Time:         eventTime,
	}
}

func (c *WSClient) liquidationFromWS(eventTime int64, raw wsLiquidation) model.Liquidation {
	if eventTime == 0 {
		eventTime = raw.TradeTime
	}
	return model.Liquidation{
		Exchange:         "bybit",
		Market:           c.category,
		Symbol:           raw.Symbol,
		Side:             raw.Side,
		OriginalQuantity: raw.Size,
		Price:            raw.Price,
		LastFilledQty:    raw.Size,
		AccumulatedQty:   raw.Size,
		TradeTime:        raw.TradeTime,
		EventTime:        eventTime,
	}
}

func parseTopic(topic string) (string, string, bool) {
	parts := strings.Split(topic, ".")
	if len(parts) != 3 || parts[0] != "kline" {
		return "", "", false
	}
	return parts[2], intervalFromBybit(parts[1]), true
}

func int64ify(value string) int64 {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func intervalFromBybit(interval string) string {
	switch interval {
	case "1":
		return "1m"
	case "3":
		return "3m"
	case "5":
		return "5m"
	case "15":
		return "15m"
	case "30":
		return "30m"
	case "60":
		return "1h"
	case "120":
		return "2h"
	case "240":
		return "4h"
	default:
		return interval
	}
}

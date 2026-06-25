package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"alphaflow/go-service/market-data/internal/model"
)

type RESTClient struct {
	baseURL    string
	httpClient HTTPClient
}

type HTTPClient interface {
	Get(ctx context.Context, rawURL string, query map[string]string) ([]byte, error)
}

func NewRESTClient(baseURL string, httpClient HTTPClient) *RESTClient {
	return &RESTClient{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func (c *RESTClient) Exchange() string {
	return "binance"
}

func (c *RESTClient) Market() string {
	return "um"
}

func (c *RESTClient) FetchKlines(
	ctx context.Context,
	symbol string,
	interval string,
	limit int,
	startTime int64,
) ([]model.Kline, error) {
	query := map[string]string{
		"symbol":   symbol,
		"interval": interval,
		"limit":    strconv.Itoa(limit),
	}
	if startTime > 0 {
		query["startTime"] = strconv.FormatInt(startTime, 10)
	}

	body, err := c.httpClient.Get(ctx, c.baseURL+"/fapi/v1/klines", query)
	if err != nil {
		return nil, fmt.Errorf("fetch klines: %w", err)
	}
	var raw [][]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode klines: %w", err)
	}

	klines := make([]model.Kline, 0, len(raw))
	for _, item := range raw {
		kline, err := parseRESTKline(symbol, interval, item)
		if err != nil {
			return nil, err
		}
		klines = append(klines, kline)
	}

	return klines, nil
}

func (c *RESTClient) FetchOpenInterest(
	ctx context.Context,
	symbol string,
) (model.OpenInterest, error) {
	body, err := c.httpClient.Get(ctx, c.baseURL+"/fapi/v1/openInterest", map[string]string{
		"symbol": symbol,
	})
	if err != nil {
		return model.OpenInterest{}, fmt.Errorf("fetch open interest: %w", err)
	}

	var raw struct {
		OpenInterest string `json:"openInterest"`
		Symbol       string `json:"symbol"`
		Time         int64  `json:"time"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return model.OpenInterest{}, fmt.Errorf("decode open interest: %w", err)
	}

	return model.OpenInterest{
		Exchange:     "binance",
		Market:       "um",
		Symbol:       raw.Symbol,
		OpenInterest: raw.OpenInterest,
		Time:         raw.Time,
	}, nil
}

func parseRESTKline(symbol string, interval string, item []any) (model.Kline, error) {
	if len(item) < 11 {
		return model.Kline{}, fmt.Errorf("invalid rest kline length: %d", len(item))
	}

	openTime, err := numberAsInt64(item[0])
	if err != nil {
		return model.Kline{}, fmt.Errorf("parse open time: %w", err)
	}
	closeTime, err := numberAsInt64(item[6])
	if err != nil {
		return model.Kline{}, fmt.Errorf("parse close time: %w", err)
	}
	tradeCount, err := numberAsInt64(item[8])
	if err != nil {
		return model.Kline{}, fmt.Errorf("parse trade count: %w", err)
	}

	return model.Kline{
		Exchange:            "binance",
		Market:              "um",
		Symbol:              symbol,
		Interval:            interval,
		OpenTime:            openTime,
		CloseTime:           closeTime,
		Open:                stringValue(item[1]),
		High:                stringValue(item[2]),
		Low:                 stringValue(item[3]),
		Close:               stringValue(item[4]),
		Volume:              stringValue(item[5]),
		QuoteVolume:         stringValue(item[7]),
		TradeCount:          tradeCount,
		TakerBuyVolume:      stringValue(item[9]),
		TakerBuyQuoteVolume: stringValue(item[10]),
		IsClosed:            true,
	}, nil
}

func numberAsInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, fmt.Errorf("unexpected number type %T", value)
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

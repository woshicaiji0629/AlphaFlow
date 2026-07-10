package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/marketmodel"
)

type HTTPClient interface {
	Get(ctx context.Context, rawURL string, query map[string]string) ([]byte, error)
}

type RESTClient struct {
	baseURL     string
	productType string
	httpClient  HTTPClient
}

type candlesResponse struct {
	Code        string     `json:"code"`
	Msg         string     `json:"msg"`
	RequestTime int64      `json:"requestTime"`
	Data        [][]string `json:"data"`
}

func NewRESTClient(baseURL string, productType string, httpClient HTTPClient) *RESTClient {
	return &RESTClient{
		baseURL:     baseURL,
		productType: productType,
		httpClient:  httpClient,
	}
}

func (c *RESTClient) Exchange() string {
	return "bitget"
}

func (c *RESTClient) Market() string {
	return strings.ToLower(c.productType)
}

func (c *RESTClient) FetchKlines(
	ctx context.Context,
	symbol string,
	interval string,
	limit int,
	startTime int64,
) ([]marketmodel.Kline, error) {
	query := map[string]string{
		"symbol":      symbol,
		"productType": c.productType,
		"granularity": bitgetInterval(interval),
		"limit":       strconv.Itoa(limit),
	}
	if startTime > 0 {
		query["startTime"] = strconv.FormatInt(startTime, 10)
	}

	body, err := c.httpClient.Get(ctx, c.baseURL+"/api/v2/mix/market/candles", query)
	if err != nil {
		return nil, fmt.Errorf("fetch candles: %w", err)
	}

	var response candlesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode candles: %w", err)
	}
	if response.Code != "00000" {
		return nil, fmt.Errorf("fetch candles: code %s msg %s", response.Code, response.Msg)
	}

	klines := make([]marketmodel.Kline, 0, len(response.Data))
	for index := len(response.Data) - 1; index >= 0; index-- {
		kline, err := c.klineFromRaw(symbol, interval, response.Data[index])
		if err != nil {
			return nil, err
		}
		if startTime > 0 && kline.OpenTime < startTime {
			continue
		}
		if response.RequestTime > 0 && kline.CloseTime >= response.RequestTime {
			continue
		}
		klines = append(klines, kline)
	}
	return klines, nil
}

func (c *RESTClient) FetchOpenInterest(
	context.Context,
	string,
) (marketmodel.OpenInterest, error) {
	return marketmodel.OpenInterest{}, nil
}

func (c *RESTClient) klineFromRaw(symbol string, interval string, raw []string) (marketmodel.Kline, error) {
	if len(raw) < 7 {
		return marketmodel.Kline{}, fmt.Errorf("invalid bitget kline length: %d", len(raw))
	}

	openTime, err := strconv.ParseInt(raw[0], 10, 64)
	if err != nil {
		return marketmodel.Kline{}, fmt.Errorf("parse open time: %w", err)
	}
	intervalMillis, err := marketmodel.IntervalMillis(interval)
	if err != nil {
		return marketmodel.Kline{}, err
	}

	return marketmodel.Kline{
		Exchange:    "bitget",
		Market:      c.Market(),
		Symbol:      symbol,
		Interval:    interval,
		OpenTime:    openTime,
		CloseTime:   openTime + intervalMillis - 1,
		Open:        raw[1],
		High:        raw[2],
		Low:         raw[3],
		Close:       raw[4],
		Volume:      raw[5],
		QuoteVolume: raw[6],
		IsClosed:    true,
	}, nil
}

func bitgetInterval(interval string) string {
	switch interval {
	case "1h":
		return "1H"
	case "2h":
		return "2H"
	case "4h":
		return "4H"
	default:
		return interval
	}
}

func Interval(interval string) string {
	return bitgetInterval(interval)
}

func KlineFromRaw(productType string, symbol string, interval string, raw []string) (marketmodel.Kline, error) {
	client := RESTClient{productType: productType}
	return client.klineFromRaw(symbol, interval, raw)
}

package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"alphaflow/go-service/market-data/internal/model"
)

type HTTPClient interface {
	Get(ctx context.Context, rawURL string, query map[string]string) ([]byte, error)
}

type RESTClient struct {
	baseURL    string
	httpClient HTTPClient
}

type candlesResponse struct {
	Code string     `json:"code"`
	Msg  string     `json:"msg"`
	Data [][]string `json:"data"`
}

func NewRESTClient(baseURL string, httpClient HTTPClient) *RESTClient {
	return &RESTClient{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func (c *RESTClient) Exchange() string {
	return "okx"
}

func (c *RESTClient) Market() string {
	return "swap"
}

func (c *RESTClient) FetchKlines(
	ctx context.Context,
	symbol string,
	interval string,
	limit int,
	startTime int64,
) ([]model.Kline, error) {
	query := map[string]string{
		"instId": symbol,
		"bar":    okxInterval(interval),
		"limit":  strconv.Itoa(limit),
	}
	if startTime > 0 {
		query["before"] = strconv.FormatInt(startTime, 10)
	}

	body, err := c.httpClient.Get(ctx, c.baseURL+"/api/v5/market/candles", query)
	if err != nil {
		return nil, fmt.Errorf("fetch candles: %w", err)
	}

	var response candlesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode candles: %w", err)
	}
	if response.Code != "0" {
		return nil, fmt.Errorf("fetch candles: code %s msg %s", response.Code, response.Msg)
	}

	klines := make([]model.Kline, 0, len(response.Data))
	for index := len(response.Data) - 1; index >= 0; index-- {
		kline, err := parseKline(symbol, interval, response.Data[index])
		if err != nil {
			return nil, err
		}
		if startTime > 0 && kline.OpenTime < startTime {
			continue
		}
		klines = append(klines, kline)
	}
	return klines, nil
}

func (c *RESTClient) FetchOpenInterest(
	context.Context,
	string,
) (model.OpenInterest, error) {
	return model.OpenInterest{}, nil
}

func parseKline(symbol string, interval string, raw []string) (model.Kline, error) {
	if len(raw) < 9 {
		return model.Kline{}, fmt.Errorf("invalid okx kline length: %d", len(raw))
	}

	openTime, err := strconv.ParseInt(raw[0], 10, 64)
	if err != nil {
		return model.Kline{}, fmt.Errorf("parse open time: %w", err)
	}
	intervalMillis, err := model.IntervalMillis(interval)
	if err != nil {
		return model.Kline{}, err
	}

	return model.Kline{
		Exchange:    "okx",
		Market:      "swap",
		Symbol:      symbol,
		Interval:    interval,
		OpenTime:    openTime,
		CloseTime:   openTime + intervalMillis - 1,
		Open:        raw[1],
		High:        raw[2],
		Low:         raw[3],
		Close:       raw[4],
		Volume:      raw[5],
		QuoteVolume: raw[7],
		IsClosed:    raw[8] == "1",
	}, nil
}

func okxInterval(interval string) string {
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

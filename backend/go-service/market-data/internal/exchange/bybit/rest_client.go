package bybit

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
	category   string
	httpClient HTTPClient
}

type klineResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List [][]string `json:"list"`
	} `json:"result"`
}

func NewRESTClient(baseURL string, category string, httpClient HTTPClient) *RESTClient {
	return &RESTClient{
		baseURL:    baseURL,
		category:   category,
		httpClient: httpClient,
	}
}

func (c *RESTClient) Exchange() string {
	return "bybit"
}

func (c *RESTClient) Market() string {
	return c.category
}

func (c *RESTClient) FetchKlines(
	ctx context.Context,
	symbol string,
	interval string,
	limit int,
	startTime int64,
) ([]model.Kline, error) {
	query := map[string]string{
		"category": c.category,
		"symbol":   symbol,
		"interval": bybitInterval(interval),
		"limit":    strconv.Itoa(limit),
	}
	if startTime > 0 {
		query["start"] = strconv.FormatInt(startTime, 10)
	}

	body, err := c.httpClient.Get(ctx, c.baseURL+"/v5/market/kline", query)
	if err != nil {
		return nil, fmt.Errorf("fetch kline: %w", err)
	}

	var response klineResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode kline: %w", err)
	}
	if response.RetCode != 0 {
		return nil, fmt.Errorf("fetch kline: code %d msg %s", response.RetCode, response.RetMsg)
	}

	klines := make([]model.Kline, 0, len(response.Result.List))
	for index := len(response.Result.List) - 1; index >= 0; index-- {
		kline, err := c.klineFromRaw(symbol, interval, response.Result.List[index])
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

func (c *RESTClient) klineFromRaw(symbol string, interval string, raw []string) (model.Kline, error) {
	if len(raw) < 7 {
		return model.Kline{}, fmt.Errorf("invalid bybit kline length: %d", len(raw))
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
		Exchange:    "bybit",
		Market:      c.category,
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

func bybitInterval(interval string) string {
	switch interval {
	case "1m":
		return "1"
	case "3m":
		return "3"
	case "5m":
		return "5"
	case "15m":
		return "15"
	case "30m":
		return "30"
	case "1h":
		return "60"
	case "2h":
		return "120"
	case "4h":
		return "240"
	default:
		return interval
	}
}

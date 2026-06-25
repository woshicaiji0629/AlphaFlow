package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type HTTPClient interface {
	Get(ctx context.Context, rawURL string, query map[string]string) ([]byte, error)
}

type RESTClient struct {
	baseURL    string
	settle     string
	httpClient HTTPClient
}

type restKline struct {
	Time        float64 `json:"t"`
	Volume      string  `json:"v"`
	Close       string  `json:"c"`
	High        string  `json:"h"`
	Low         string  `json:"l"`
	Open        string  `json:"o"`
	QuoteVolume string  `json:"sum"`
}

func NewRESTClient(baseURL string, settle string, httpClient HTTPClient) *RESTClient {
	return &RESTClient{
		baseURL:    baseURL,
		settle:     settle,
		httpClient: httpClient,
	}
}

func (c *RESTClient) Exchange() string {
	return "gate"
}

func (c *RESTClient) Market() string {
	return c.settle
}

func (c *RESTClient) FetchKlines(
	ctx context.Context,
	symbol string,
	interval string,
	limit int,
	startTime int64,
) ([]model.Kline, error) {
	query := map[string]string{
		"contract": symbol,
		"interval": interval,
	}
	if startTime > 0 {
		query["from"] = strconv.FormatInt(startTime/1000, 10)
		query["to"] = strconv.FormatInt(time.Now().Unix(), 10)
	} else {
		query["limit"] = strconv.Itoa(limit)
	}

	body, err := c.httpClient.Get(ctx, c.baseURL+"/futures/"+c.settle+"/candlesticks", query)
	if err != nil {
		return nil, fmt.Errorf("fetch candlesticks: %w", err)
	}

	var raw []restKline
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode candlesticks: %w", err)
	}

	klines := make([]model.Kline, 0, len(raw))
	for _, item := range raw {
		kline, err := c.klineFromREST(symbol, interval, item)
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

func (c *RESTClient) klineFromREST(symbol string, interval string, raw restKline) (model.Kline, error) {
	openTime := int64(raw.Time * 1000)
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
		IsClosed:    true,
	}, nil
}

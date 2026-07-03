package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
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
	Time        float64    `json:"t"`
	Volume      flexString `json:"v"`
	Close       flexString `json:"c"`
	High        flexString `json:"h"`
	Low         flexString `json:"l"`
	Open        flexString `json:"o"`
	QuoteVolume flexString `json:"sum"`
}

type flexString string

func (s *flexString) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = flexString(text)
		return nil
	}

	var number float64
	if err := json.Unmarshal(data, &number); err == nil {
		*s = flexString(strconv.FormatFloat(number, 'f', -1, 64))
		return nil
	}

	return fmt.Errorf("unexpected value for string field: %s", string(data))
}

func (s flexString) String() string {
	return string(s)
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
) ([]marketmodel.Kline, error) {
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

	klines := make([]marketmodel.Kline, 0, len(raw))
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
) (marketmodel.OpenInterest, error) {
	return marketmodel.OpenInterest{}, nil
}

func (c *RESTClient) klineFromREST(symbol string, interval string, raw restKline) (marketmodel.Kline, error) {
	openTime := int64(raw.Time * 1000)
	intervalMillis, err := marketmodel.IntervalMillis(interval)
	if err != nil {
		return marketmodel.Kline{}, err
	}

	return marketmodel.Kline{
		Exchange:    "gate",
		Market:      c.settle,
		Symbol:      symbol,
		Interval:    interval,
		OpenTime:    openTime,
		CloseTime:   openTime + intervalMillis - 1,
		Open:        raw.Open.String(),
		High:        raw.High.String(),
		Low:         raw.Low.String(),
		Close:       raw.Close.String(),
		Volume:      raw.Volume.String(),
		QuoteVolume: raw.QuoteVolume.String(),
		IsClosed:    true,
	}, nil
}

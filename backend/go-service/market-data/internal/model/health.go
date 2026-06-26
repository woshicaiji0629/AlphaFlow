package model

import "fmt"

const (
	HealthStatusOK      = "ok"
	HealthStatusStale   = "stale"
	HealthStatusGap     = "gap"
	HealthStatusMissing = "missing"
	HealthStatusSkipped = "skipped"
)

type DataHealth struct {
	Exchange              string `json:"exchange"`
	Market                string `json:"market"`
	Symbol                string `json:"symbol"`
	Interval              string `json:"interval"`
	KlineStatus           string `json:"kline_status"`
	IndicatorStatus       string `json:"indicator_status"`
	LastKlineOpenTime     int64  `json:"last_kline_open_time,omitempty"`
	LastIndicatorOpenTime int64  `json:"last_indicator_open_time,omitempty"`
	Reason                string `json:"reason,omitempty"`
	UpdatedAt             int64  `json:"updated_at"`
}

func DataHealthKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:health:%s:%s", exchangeCode(exchange), market, symbol, interval)
}

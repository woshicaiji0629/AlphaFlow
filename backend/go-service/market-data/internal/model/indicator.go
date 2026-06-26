package model

import "fmt"

type IndicatorSnapshot struct {
	Exchange  string            `json:"exchange"`
	Market    string            `json:"market"`
	Symbol    string            `json:"symbol"`
	Interval  string            `json:"interval"`
	OpenTime  int64             `json:"open_time"`
	CloseTime int64             `json:"close_time"`
	Values    map[string]string `json:"values"`
	Signals   map[string]string `json:"signals,omitempty"`
	UpdatedAt int64             `json:"updated_at"`
}

func IndicatorKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:ind:%s:%s", exchangeCode(exchange), market, symbol, interval)
}

func IndicatorLastKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:ind:last:%s:%s", exchangeCode(exchange), market, symbol, interval)
}

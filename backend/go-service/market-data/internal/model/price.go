package model

import "fmt"

type LastPrice struct {
	Exchange  string `json:"exchange"`
	Market    string `json:"market"`
	Symbol    string `json:"symbol"`
	Price     string `json:"price"`
	Quantity  string `json:"quantity"`
	EventTime int64  `json:"event_time"`
	TradeTime int64  `json:"trade_time"`
	TradeID   int64  `json:"trade_id"`
}

type MarkPrice struct {
	Exchange        string `json:"exchange"`
	Market          string `json:"market"`
	Symbol          string `json:"symbol"`
	MarkPrice       string `json:"mark_price"`
	IndexPrice      string `json:"index_price"`
	FundingRate     string `json:"funding_rate"`
	NextFundingTime int64  `json:"next_funding_time"`
	EventTime       int64  `json:"event_time"`
}

func LastPriceKey(exchange string, market string, symbol string) string {
	return fmt.Sprintf("%s:%s:lp:%s", exchangeCode(exchange), market, symbol)
}

func MarkPriceKey(exchange string, market string, symbol string) string {
	return fmt.Sprintf("%s:%s:mp:%s", exchangeCode(exchange), market, symbol)
}

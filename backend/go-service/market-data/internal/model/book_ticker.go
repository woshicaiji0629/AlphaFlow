package model

import "fmt"

type BookTicker struct {
	Exchange        string `json:"exchange"`
	Market          string `json:"market"`
	Symbol          string `json:"symbol"`
	UpdateID        int64  `json:"update_id"`
	BidPrice        string `json:"bid_price"`
	BidQuantity     string `json:"bid_quantity"`
	AskPrice        string `json:"ask_price"`
	AskQuantity     string `json:"ask_quantity"`
	EventTime       int64  `json:"event_time"`
	TransactionTime int64  `json:"transaction_time"`
}

func BookTickerKey(exchange string, market string, symbol string) string {
	return fmt.Sprintf("%s:%s:bt:%s", exchangeCode(exchange), market, symbol)
}

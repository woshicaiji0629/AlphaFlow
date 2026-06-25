package model

import "fmt"

type OpenInterest struct {
	Exchange     string `json:"exchange"`
	Market       string `json:"market"`
	Symbol       string `json:"symbol"`
	OpenInterest string `json:"open_interest"`
	Time         int64  `json:"time"`
}

func OpenInterestKey(exchange string, market string, symbol string) string {
	return fmt.Sprintf("%s:%s:oi:%s", exchangeCode(exchange), market, symbol)
}

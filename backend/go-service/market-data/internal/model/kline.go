package model

import (
	"fmt"

	"alphaflow/go-service/pkg/marketmodel"
)

type Kline = marketmodel.Kline

func RedisKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:k:%s:%s", exchangeCode(exchange), market, symbol, interval)
}

func exchangeCode(exchange string) string {
	switch exchange {
	case "binance":
		return "bn"
	default:
		return exchange
	}
}

func IntervalMillis(interval string) (int64, error) {
	return marketmodel.IntervalMillis(interval)
}

package model

import (
	"alphaflow/go-service/pkg/marketkeys"
	"alphaflow/go-service/pkg/marketmodel"
)

type Kline = marketmodel.Kline

func RedisKey(exchange string, market string, symbol string, interval string) string {
	return marketkeys.KlineKey(exchange, market, symbol, interval)
}

func exchangeCode(exchange string) string {
	return marketkeys.ExchangeCode(exchange)
}

func IntervalMillis(interval string) (int64, error) {
	return marketmodel.IntervalMillis(interval)
}

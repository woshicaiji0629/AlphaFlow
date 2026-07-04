package model

import (
	"alphaflow/go-service/pkg/marketkeys"
	"alphaflow/go-service/pkg/marketmodel"
)

type IndicatorSnapshot = marketmodel.IndicatorSnapshot
type IndicatorWindowSnapshot = marketmodel.IndicatorWindowSnapshot
type IndicatorRealtimeSnapshot = marketmodel.IndicatorRealtimeSnapshot

func IndicatorKey(exchange string, market string, symbol string, interval string) string {
	return marketkeys.IndicatorKey(exchange, market, symbol, interval)
}

func IndicatorLastKey(exchange string, market string, symbol string, interval string) string {
	return marketkeys.IndicatorLastKey(exchange, market, symbol, interval)
}

func IndicatorWindowKey(exchange string, market string, symbol string, interval string) string {
	return marketkeys.IndicatorWindowKey(exchange, market, symbol, interval)
}

func IndicatorWindowLatestKey(exchange string, market string, symbol string, interval string) string {
	return marketkeys.IndicatorWindowLatestKey(exchange, market, symbol, interval)
}

func IndicatorWindowLastKey(exchange string, market string, symbol string, interval string) string {
	return marketkeys.IndicatorWindowLastKey(exchange, market, symbol, interval)
}

func IndicatorRealtimeKey(exchange string, market string, symbol string, interval string) string {
	return marketkeys.IndicatorRealtimeKey(exchange, market, symbol, interval)
}

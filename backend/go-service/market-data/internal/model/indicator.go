package model

import (
	"fmt"

	"alphaflow/go-service/pkg/marketmodel"
)

type IndicatorSnapshot = marketmodel.IndicatorSnapshot
type IndicatorWindowSnapshot = marketmodel.IndicatorWindowSnapshot
type IndicatorRealtimeSnapshot = marketmodel.IndicatorRealtimeSnapshot

func IndicatorKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:ind:%s:%s", exchangeCode(exchange), market, symbol, interval)
}

func IndicatorLastKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:ind:last:%s:%s", exchangeCode(exchange), market, symbol, interval)
}

func IndicatorWindowKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:indwin:%s:%s", exchangeCode(exchange), market, symbol, interval)
}

func IndicatorWindowLatestKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:indwin:latest:%s:%s", exchangeCode(exchange), market, symbol, interval)
}

func IndicatorWindowLastKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:indwin:last:%s:%s", exchangeCode(exchange), market, symbol, interval)
}

func IndicatorRealtimeKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:indrt:%s:%s", exchangeCode(exchange), market, symbol, interval)
}

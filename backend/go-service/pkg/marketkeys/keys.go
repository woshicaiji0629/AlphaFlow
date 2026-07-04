package marketkeys

import "fmt"

func KlineKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:k:%s:%s", ExchangeCode(exchange), market, symbol, interval)
}

func IndicatorKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:ind:%s:%s", ExchangeCode(exchange), market, symbol, interval)
}

func IndicatorLastKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:ind:last:%s:%s", ExchangeCode(exchange), market, symbol, interval)
}

func IndicatorWindowKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:indwin:%s:%s", ExchangeCode(exchange), market, symbol, interval)
}

func IndicatorWindowLatestKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:indwin:latest:%s:%s", ExchangeCode(exchange), market, symbol, interval)
}

func IndicatorWindowLastKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:indwin:last:%s:%s", ExchangeCode(exchange), market, symbol, interval)
}

func IndicatorRealtimeKey(exchange string, market string, symbol string, interval string) string {
	return fmt.Sprintf("%s:%s:indrt:%s:%s", ExchangeCode(exchange), market, symbol, interval)
}

func LastPriceKey(exchange string, market string, symbol string) string {
	return fmt.Sprintf("%s:%s:lp:%s", ExchangeCode(exchange), market, symbol)
}

func MarkPriceKey(exchange string, market string, symbol string) string {
	return fmt.Sprintf("%s:%s:mp:%s", ExchangeCode(exchange), market, symbol)
}

func ExchangeCode(exchange string) string {
	switch exchange {
	case "binance":
		return "bn"
	default:
		return exchange
	}
}

package config

import "time"

func RESTLimit() int {
	return 200
}

func MarkPriceInterval() string {
	return "1s"
}

func OpenInterestInterval() time.Duration {
	return time.Minute
}

func BinanceIntervals() []string {
	return []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h"}
}

func BinanceRESTBase() string {
	return "https://fapi.binance.com"
}

func BinanceWSBase() string {
	return "wss://fstream.binance.com"
}

func GateIntervals() []string {
	return []string{"1m", "5m", "15m", "30m", "1h", "4h"}
}

func GateRESTBase() string {
	return "https://api.gateio.ws/api/v4"
}

func GateWSBase() string {
	return "wss://fx-ws.gateio.ws/v4/ws/usdt"
}

func GateSettle() string {
	return "usdt"
}

func BitgetIntervals() []string {
	return []string{"1m", "5m", "15m", "30m", "1h", "4h"}
}

func BitgetRESTBase() string {
	return "https://api.bitget.com"
}

func BitgetWSBase() string {
	return "wss://ws.bitget.com/v2/ws/public"
}

func BitgetProductType() string {
	return "USDT-FUTURES"
}

func BybitIntervals() []string {
	return []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h"}
}

func BybitRESTBase() string {
	return "https://api.bybit.com"
}

func BybitWSBase() string {
	return "wss://stream.bybit.com/v5/public/linear"
}

func BybitCategory() string {
	return "linear"
}

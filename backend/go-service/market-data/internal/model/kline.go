package model

import "fmt"

type Kline struct {
	Exchange            string `json:"exchange"`
	Market              string `json:"market"`
	Symbol              string `json:"symbol"`
	Interval            string `json:"interval"`
	OpenTime            int64  `json:"open_time"`
	CloseTime           int64  `json:"close_time"`
	Open                string `json:"open"`
	High                string `json:"high"`
	Low                 string `json:"low"`
	Close               string `json:"close"`
	Volume              string `json:"volume"`
	QuoteVolume         string `json:"quote_volume"`
	TradeCount          int64  `json:"trade_count"`
	TakerBuyVolume      string `json:"taker_buy_volume"`
	TakerBuyQuoteVolume string `json:"taker_buy_quote_volume"`
	IsClosed            bool   `json:"is_closed"`
	EventTime           int64  `json:"event_time,omitempty"`
	FirstTradeID        int64  `json:"first_trade_id,omitempty"`
	LastTradeID         int64  `json:"last_trade_id,omitempty"`
	Ignore              string `json:"-"`
}

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
	switch interval {
	case "1m":
		return 60 * 1000, nil
	case "3m":
		return 3 * 60 * 1000, nil
	case "5m":
		return 5 * 60 * 1000, nil
	case "15m":
		return 15 * 60 * 1000, nil
	case "30m":
		return 30 * 60 * 1000, nil
	case "1h":
		return 60 * 60 * 1000, nil
	case "2h":
		return 2 * 60 * 60 * 1000, nil
	case "4h":
		return 4 * 60 * 60 * 1000, nil
	default:
		return 0, fmt.Errorf("unsupported interval %q", interval)
	}
}

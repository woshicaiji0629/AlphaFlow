package model

import "fmt"

type Liquidation struct {
	Exchange         string `json:"exchange"`
	Market           string `json:"market"`
	Symbol           string `json:"symbol"`
	Side             string `json:"side"`
	OrderType        string `json:"order_type"`
	TimeInForce      string `json:"time_in_force"`
	OriginalQuantity string `json:"original_quantity"`
	Price            string `json:"price"`
	AveragePrice     string `json:"average_price"`
	OrderStatus      string `json:"order_status"`
	LastFilledQty    string `json:"last_filled_qty"`
	AccumulatedQty   string `json:"accumulated_qty"`
	TradeTime        int64  `json:"trade_time"`
	EventTime        int64  `json:"event_time"`
}

func LiquidationKey(exchange string, market string, symbol string) string {
	return fmt.Sprintf("%s:%s:liq:%s", exchangeCode(exchange), market, symbol)
}

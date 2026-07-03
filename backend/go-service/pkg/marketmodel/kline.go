package marketmodel

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

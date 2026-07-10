package model

import "fmt"

type MarketStatus struct {
	Exchange  string `json:"exchange"`
	Market    string `json:"market"`
	Symbol    string `json:"symbol,omitempty"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

func MarketStatusKey(exchange string, market string, symbol ...string) string {
	key := fmt.Sprintf("%s:%s:status", exchangeCode(exchange), market)
	if len(symbol) > 0 && symbol[0] != "" {
		return key + ":" + symbol[0]
	}
	return key
}

type WebSocketStatus struct {
	Exchange            string `json:"exchange"`
	Market              string `json:"market"`
	Shard               string `json:"shard,omitempty"`
	Connected           bool   `json:"connected"`
	LastError           string `json:"last_error,omitempty"`
	LastStartedAt       int64  `json:"last_started_at,omitempty"`
	LastStoppedAt       int64  `json:"last_stopped_at,omitempty"`
	StreamCount         int    `json:"stream_count,omitempty"`
	ConnectionCount     int    `json:"connection_count,omitempty"`
	ReconnectCount      int64  `json:"reconnect_count"`
	ConsecutiveFailures int64  `json:"consecutive_failures"`
	UpdatedAt           int64  `json:"updated_at"`
}

func WebSocketStatusKey(exchange string, market string) string {
	return fmt.Sprintf("%s:%s:ws", exchangeCode(exchange), market)
}

func WebSocketShardStatusKey(exchange string, market string, shard string) string {
	return fmt.Sprintf("%s:%s:ws:%s", exchangeCode(exchange), market, shard)
}

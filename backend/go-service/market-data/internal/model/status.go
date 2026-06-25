package model

import "fmt"

type MarketStatus struct {
	Exchange  string `json:"exchange"`
	Market    string `json:"market"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

func MarketStatusKey(exchange string, market string) string {
	return fmt.Sprintf("%s:%s:status", exchangeCode(exchange), market)
}

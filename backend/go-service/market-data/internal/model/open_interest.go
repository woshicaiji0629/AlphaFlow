package model

import (
	"fmt"

	"alphaflow/go-service/pkg/marketmodel"
)

type OpenInterest = marketmodel.OpenInterest

func OpenInterestKey(exchange string, market string, symbol string) string {
	return fmt.Sprintf("%s:%s:oi:%s", exchangeCode(exchange), market, symbol)
}

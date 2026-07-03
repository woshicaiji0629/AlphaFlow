package store

import (
	"context"

	"alphaflow/go-service/pkg/clickhousemarket"
)

type ClickHouseOptions = clickhousemarket.Options
type ClickHouseStore = clickhousemarket.Store

func NewClickHouseStore(ctx context.Context, options ClickHouseOptions) (*ClickHouseStore, error) {
	return clickhousemarket.NewStore(ctx, options)
}

package exchange

import (
	"context"

	"alphaflow/go-service/market-data/internal/model"
)

type Stream struct {
	Symbol   string
	Interval string
	Type     StreamType
}

type StreamType string

const (
	StreamTypeKline      StreamType = "kline"
	StreamTypeAggTrade   StreamType = "agg_trade"
	StreamTypeMarkPrice  StreamType = "mark_price"
	StreamTypeBookTicker StreamType = "book_ticker"
	StreamTypeForceOrder StreamType = "force_order"
)

type Handler interface {
	HandleKline(context.Context, model.Kline) error
	HandleLastPrice(context.Context, model.LastPrice) error
	HandleMarkPrice(context.Context, model.MarkPrice) error
	HandleBookTicker(context.Context, model.BookTicker) error
	HandleOpenInterest(context.Context, model.OpenInterest) error
	HandleLiquidation(context.Context, model.Liquidation) error
}

type RESTClient interface {
	Exchange() string
	Market() string
	FetchKlines(
		ctx context.Context,
		symbol string,
		interval string,
		limit int,
		startTime int64,
	) ([]model.Kline, error)
	FetchOpenInterest(ctx context.Context, symbol string) (model.OpenInterest, error)
}

type WSClient interface {
	Run(ctx context.Context, streams []Stream, handler Handler) error
}

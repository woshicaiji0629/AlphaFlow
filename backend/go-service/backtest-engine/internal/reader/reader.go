package reader

import (
	"context"
	"fmt"

	"alphaflow/go-service/pkg/marketmodel"
)

type Request struct {
	Exchange string
	Market   string
	Symbol   string
	Interval string
	Start    int64
	End      int64
}

type KlineStore interface {
	RangeKlines(
		ctx context.Context,
		exchange string,
		market string,
		symbol string,
		interval string,
		start int64,
		end int64,
	) ([]marketmodel.Kline, error)
}

type Reader struct {
	store KlineStore
}

func New(store KlineStore) (*Reader, error) {
	if store == nil {
		return nil, fmt.Errorf("kline store is required")
	}
	return &Reader{store: store}, nil
}

func (r *Reader) ReadKlines(ctx context.Context, request Request) ([]marketmodel.Kline, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	klines, err := r.store.RangeKlines(
		ctx,
		request.Exchange,
		request.Market,
		request.Symbol,
		request.Interval,
		request.Start,
		request.End,
	)
	if err != nil {
		return nil, fmt.Errorf("read historical klines: %w", err)
	}
	return klines, nil
}

func validateRequest(request Request) error {
	if request.Exchange == "" {
		return fmt.Errorf("exchange cannot be empty")
	}
	if request.Market == "" {
		return fmt.Errorf("market cannot be empty")
	}
	if request.Symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}
	if request.Interval == "" {
		return fmt.Errorf("interval cannot be empty")
	}
	if request.End < request.Start {
		return fmt.Errorf("end must be greater than or equal to start")
	}
	return nil
}

package execution

import (
	"context"
	"fmt"
	"strconv"
)

type PaperBroker struct {
	price       string
	now         func() int64
	slippageBps float64
}

type PaperBrokerOptions struct {
	Price       string
	Now         func() int64
	SlippageBps float64
}

func NewPaperBroker(price string, now func() int64) *PaperBroker {
	return NewPaperBrokerWithOptions(PaperBrokerOptions{
		Price: price,
		Now:   now,
	})
}

func NewPaperBrokerWithOptions(options PaperBrokerOptions) *PaperBroker {
	now := options.Now
	if now == nil {
		now = func() int64 { return 0 }
	}
	slippageBps := options.SlippageBps
	if slippageBps < 0 {
		slippageBps = 0
	}
	return &PaperBroker{
		price:       options.Price,
		now:         now,
		slippageBps: slippageBps,
	}
}

func (b *PaperBroker) Execute(ctx context.Context, intent OrderIntent) (ExecutionReport, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionReport{}, err
	}
	if intent.Type != OrderTypeMarket {
		return rejectedReport(intent, b.now(), "paper broker only supports market orders"), nil
	}
	if intent.Quantity <= 0 {
		return rejectedReport(intent, b.now(), "quantity must be positive"), nil
	}
	price := b.fillPrice(intent)
	if price == "" {
		return rejectedReport(intent, b.now(), "fill price is required"), nil
	}
	return ExecutionReport{
		IntentID:        intent.IntentID,
		ExchangeOrderID: fmt.Sprintf("paper:%s", intent.IntentID),
		Status:          ExecutionStatusFilled,
		FilledQuantity:  intent.Quantity,
		AveragePrice:    price,
		UpdatedAt:       b.now(),
	}, nil
}

func (b *PaperBroker) Recover(ctx context.Context, intent OrderIntent) (ExecutionReport, bool, error) {
	report, err := b.Execute(ctx, intent)
	return report, err == nil, err
}

func (b *PaperBroker) fillPrice(intent OrderIntent) string {
	price := b.price
	if price == "" {
		price = intent.ReferencePrice
	}
	if price == "" || b.slippageBps <= 0 {
		return price
	}
	parsed, err := strconv.ParseFloat(price, 64)
	if err != nil || parsed <= 0 {
		return price
	}
	switch intent.Side {
	case OrderSideBuy:
		parsed *= 1 + b.slippageBps/10000
	case OrderSideSell:
		parsed *= 1 - b.slippageBps/10000
	}
	return strconv.FormatFloat(parsed, 'f', -1, 64)
}

func rejectedReport(intent OrderIntent, updatedAt int64, reason string) ExecutionReport {
	return ExecutionReport{
		IntentID:  intent.IntentID,
		Status:    ExecutionStatusRejected,
		Error:     reason,
		UpdatedAt: updatedAt,
	}
}

package execution

import (
	"context"
	"fmt"
)

type PaperBroker struct {
	price string
	now   func() int64
}

func NewPaperBroker(price string, now func() int64) *PaperBroker {
	if now == nil {
		now = func() int64 { return 0 }
	}
	return &PaperBroker{
		price: price,
		now:   now,
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
	if b.price == "" {
		return rejectedReport(intent, b.now(), "fill price is required"), nil
	}
	return ExecutionReport{
		IntentID:        intent.IntentID,
		ExchangeOrderID: fmt.Sprintf("paper:%s", intent.IntentID),
		Status:          ExecutionStatusFilled,
		FilledQuantity:  intent.Quantity,
		AveragePrice:    b.price,
		UpdatedAt:       b.now(),
	}, nil
}

func rejectedReport(intent OrderIntent, updatedAt int64, reason string) ExecutionReport {
	return ExecutionReport{
		IntentID:  intent.IntentID,
		Status:    ExecutionStatusRejected,
		Error:     reason,
		UpdatedAt: updatedAt,
	}
}

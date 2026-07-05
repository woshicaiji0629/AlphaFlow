package symbolspec

import "testing"

func TestNormalizeQuoteOrderBaseQuantity(t *testing.T) {
	order, err := NormalizeQuoteOrder(Capability{
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		QuantityUnit: QuantityUnitBase,
		QuantityStep: 0.001,
		MinQuantity:  0.001,
		MinNotional:  5,
		ContractSize: 1,
	}, 2000, 100)
	if err != nil {
		t.Fatalf("NormalizeQuoteOrder() error = %v", err)
	}
	if order.Quantity != 0.05 || order.Notional != 100 {
		t.Fatalf("order = %#v, want quantity 0.05 notional 100", order)
	}
}

func TestNormalizeQuoteOrderContractQuantity(t *testing.T) {
	order, err := NormalizeQuoteOrder(Capability{
		Exchange:     "gate",
		Market:       "um",
		Symbol:       "BTCUSDT",
		QuantityUnit: QuantityUnitContract,
		QuantityStep: 1,
		MinQuantity:  1,
		MinNotional:  5,
		ContractSize: 0.001,
	}, 50000, 100)
	if err != nil {
		t.Fatalf("NormalizeQuoteOrder() error = %v", err)
	}
	if order.Quantity != 2 || order.Notional != 100 {
		t.Fatalf("order = %#v, want 2 contracts notional 100", order)
	}
}

func TestNormalizeQuoteOrderRejectsBelowMinNotional(t *testing.T) {
	_, err := NormalizeQuoteOrder(Capability{
		Exchange:     "binance",
		Market:       "spot",
		Symbol:       "ETHUSDT",
		QuantityUnit: QuantityUnitBase,
		QuantityStep: 0.001,
		MinNotional:  5,
		ContractSize: 1,
	}, 2000, 1)
	if err == nil {
		t.Fatal("NormalizeQuoteOrder() error = nil, want min notional error")
	}
}

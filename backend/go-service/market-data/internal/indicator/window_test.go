package indicator

import (
	"testing"

	"alphaflow/go-service/market-data/internal/model"
)

func TestCalculationWindowCloneIsIndependent(t *testing.T) {
	window := NewCalculationWindowFromKlines([]model.Kline{
		{
			OpenTime: 1000,
			Open:     "1",
			High:     "2",
			Low:      "1",
			Close:    "2",
			Volume:   "10",
			IsClosed: true,
		},
	}, 10)

	clone := window.Clone()
	window.Append([]model.Kline{
		{
			OpenTime: 2000,
			Open:     "2",
			High:     "3",
			Low:      "2",
			Close:    "3",
			Volume:   "11",
			IsClosed: true,
		},
	})

	if len(clone.Klines()) != 1 {
		t.Fatalf("clone klines = %d, want 1", len(clone.Klines()))
	}
	if got := clone.Klines()[0].OpenTime; got != 1000 {
		t.Fatalf("clone first open time = %d, want 1000", got)
	}
	_, _, _, closes, _, err := clone.Series()
	if err != nil {
		t.Fatalf("clone series: %v", err)
	}
	if len(closes) != 1 || closes[0] != 2 {
		t.Fatalf("clone closes = %#v, want [2]", closes)
	}
}

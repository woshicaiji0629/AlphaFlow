package indicatorcalc

import (
	"testing"

	model "alphaflow/go-service/pkg/marketmodel"
)

var benchmarkCalculateResult Result

func BenchmarkCalculate120Bars(b *testing.B) {
	benchmarkCalculate(b, 120)
}

func BenchmarkCalculate250Bars(b *testing.B) {
	benchmarkCalculate(b, 250)
}

func BenchmarkCalculate300Bars(b *testing.B) {
	benchmarkCalculate(b, 300)
}

func benchmarkCalculate(b *testing.B, bars int) {
	klines := benchmarkKlines(bars)
	options := DefaultOptions()

	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := Calculate(klines, options)
		if err != nil {
			b.Fatalf("Calculate: %v", err)
		}
		benchmarkCalculateResult = result
	}
}

func benchmarkKlines(count int) []model.Kline {
	klines := make([]model.Kline, 0, count)
	for index := 0; index < count; index++ {
		price := 100 + float64(index%90) + float64(index/90)*3
		klines = append(klines, testKline(int64(index), price, true))
	}
	return klines
}

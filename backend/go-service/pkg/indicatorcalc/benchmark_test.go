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

func BenchmarkCalculateWindowStreaming(b *testing.B) {
	klines := benchmarkKlines(4396)
	window := NewCalculationWindowFromKlines(klines[:300], 268)
	window.EnableBasicState()
	if _, err := CalculateWindow(window, DefaultOptions()); err != nil {
		b.Fatalf("seed CalculateWindow: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		window.Append([]model.Kline{klines[300+index%4096]})
		result, err := CalculateWindow(window, DefaultOptions())
		if err != nil {
			b.Fatalf("CalculateWindow: %v", err)
		}
		benchmarkCalculateResult = result
	}
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

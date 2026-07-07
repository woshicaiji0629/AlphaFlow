package indicator

import (
	"testing"

	"alphaflow/go-service/pkg/indicatorcalc"
)

var benchmarkRealtimeWindowResult indicatorcalc.Result

func BenchmarkWindowWithTemporaryKlineRealtime(b *testing.B) {
	klines := minuteKlines(250)
	window := newCalculationWindowFromKlines(klines, 250)
	open := minuteKline(int64(len(klines)), 350)
	open.IsClosed = false
	options := indicatorcalc.DefaultOptions()

	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		calcWindow := windowWithTemporaryKline(window, open, 250)
		result, err := indicatorcalc.CalculateWindow(calcWindow, options)
		if err != nil {
			b.Fatalf("CalculateWindow: %v", err)
		}
		benchmarkRealtimeWindowResult = result
	}
}

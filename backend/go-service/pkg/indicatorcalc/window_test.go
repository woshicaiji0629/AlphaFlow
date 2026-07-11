package indicatorcalc

import (
	"testing"

	model "alphaflow/go-service/pkg/marketmodel"
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

func TestCalculationWindowCloneForAppendIsIndependent(t *testing.T) {
	window := NewCalculationWindowFromKlines(testWindowKlines(3), 10)
	window.EnableBasicState()
	clone := window.CloneForAppend()
	clone.Append(testWindowKlines(1))
	if len(window.Klines()) != 3 {
		t.Fatalf("source klines = %d, want 3", len(window.Klines()))
	}
	if len(clone.Klines()) != 4 {
		t.Fatalf("clone klines = %d, want 4", len(clone.Klines()))
	}
}

func TestCalculationWindowCloneSharesImmutableAISourcePrefix(t *testing.T) {
	window := NewCalculationWindowFromKlines(testWindowKlines(3), 3)
	window.aiPrefix = &aiSourceState{allBank: []aiSourceRow{{outcome: 1}}, sourceEMA: newAISourceEMAState(3)}
	clone := window.Clone()
	if clone.aiPrefix != window.aiPrefix {
		t.Fatal("clone did not share immutable AI prefix")
	}
	window.Append(testWindowKlines(1))
	if window.aiPrefix != nil {
		t.Fatal("append did not invalidate AI prefix")
	}
}

func TestCalculationWindowPreparesAISourcePrefixOnce(t *testing.T) {
	opens, highs, lows, closes := aiSourceTestOHLC(250)
	klines := make([]model.Kline, 0, 250)
	for index := range closes {
		klines = append(klines, model.Kline{OpenTime: int64(index), Open: format(opens[index]), High: format(highs[index]), Low: format(lows[index]), Close: format(closes[index]), Volume: "10", IsClosed: true})
	}
	window := NewCalculationWindowFromKlines(klines, 250)
	if !window.prepareAISourcePrefix() || window.aiPrefix == nil {
		t.Fatal("AI source prefix was not prepared")
	}
	if got := window.aiPrefix.lineCount; got != 249 {
		t.Fatalf("AI source prefix lines = %d, want 249", got)
	}
	prefix := window.aiPrefix
	if !window.prepareAISourcePrefix() || window.aiPrefix != prefix {
		t.Fatal("AI source prefix cache was rebuilt on cache hit")
	}
}

func TestCalculationWindowPreviewAISourceMatchesBatch(t *testing.T) {
	opens, highs, lows, closes := aiSourceTestOHLC(250)
	klines := make([]model.Kline, 0, 249)
	for index := 0; index < 249; index++ {
		klines = append(klines, model.Kline{OpenTime: int64(index), Open: format(opens[index]), High: format(highs[index]), Low: format(lows[index]), Close: format(closes[index]), Volume: "10", IsClosed: true})
	}
	window := NewCalculationWindowFromKlines(klines, 250)
	preview := model.Kline{OpenTime: 249, Open: format(opens[249]), High: format(highs[249]), Low: format(lows[249]), Close: format(closes[249]), Volume: "10"}
	got, ok := window.previewAISource(preview)
	if !ok {
		t.Fatal("preview AI source unavailable")
	}
	want, ok := aiSourceSwitching(opens, highs, lows, closes, defaultAISourceConfig())
	if !ok {
		t.Fatal("batch AI source unavailable")
	}
	assertFloatClose(t, "ma", got.ma, want.ma)
	assertFloatClose(t, "source", got.sourceValue, want.sourceValue)
	assertFloatClose(t, "drive", got.drive, want.drive)
	assertFloatClose(t, "score open", got.scoreOpen, want.scoreOpen)
	assertFloatClose(t, "score high", got.scoreHigh, want.scoreHigh)
	assertFloatClose(t, "score low", got.scoreLow, want.scoreLow)
	assertFloatClose(t, "score close", got.scoreClose, want.scoreClose)
	assertFloatClose(t, "supertrend", got.supertrend, want.supertrend)
	assertFloatClose(t, "distance", got.supertrendDist, want.supertrendDist)
	assertFloatClose(t, "multiplier", got.adaptMultiplier, want.adaptMultiplier)
	if got.selected != want.selected || got.changed != want.changed || got.direction != want.direction || got.flip != want.flip || got.ready != want.ready {
		t.Fatalf("preview signals=%#v want=%#v", got, want)
	}
}

func testWindowKlines(count int) []model.Kline {
	klines := make([]model.Kline, 0, count)
	for index := 0; index < count; index++ {
		klines = append(klines, model.Kline{
			OpenTime: int64(index + 1), Open: "1", High: "2", Low: "1", Close: "2", Volume: "10", IsClosed: true,
		})
	}
	return klines
}

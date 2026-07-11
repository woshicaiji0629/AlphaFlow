package research

import (
	"math"
	"testing"
)

func TestSummarizeUsesAskEntryPnL(t *testing.T) {
	got := Summarize([]Observation{{Symbol: "BTC", Duration: "5m", Outcome: "up", SecondsToExpiry: 60, EntryPrice: .6, Won: true, Spread: .02}, {Symbol: "BTC", Duration: "5m", Outcome: "up", SecondsToExpiry: 60, EntryPrice: .4, Won: false, Spread: .04}})
	if len(got) != 1 {
		t.Fatal(len(got))
	}
	if math.Abs(got[0].PnL-0) < 1e-9 == false || got[0].Wins != 1 || math.Abs(got[0].AverageSpread-.03) > 1e-9 {
		t.Fatalf("%+v", got[0])
	}
}

func TestSummarizeSeparatesOutcomesAndEntryTimes(t *testing.T) {
	got := Summarize([]Observation{
		{Symbol: "BTC", Duration: "5m", Outcome: "up", SecondsToExpiry: 60},
		{Symbol: "BTC", Duration: "5m", Outcome: "down", SecondsToExpiry: 60},
		{Symbol: "BTC", Duration: "5m", Outcome: "up", SecondsToExpiry: 300},
	})
	if len(got) != 3 {
		t.Fatalf("buckets=%+v", got)
	}
}

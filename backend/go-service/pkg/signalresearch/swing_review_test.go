package signalresearch

import (
	"alphaflow/go-service/pkg/marketmodel"
	"testing"
)

func TestSwingMoveBucketUsesMutuallyExclusiveBoundaries(t *testing.T) {
	tests := map[float64]string{
		29.99: "below_30", 30: "30_60", 59.99: "30_60", 60: "60_100",
		99.99: "60_100", 100: "100_150", 149.99: "100_150", 150: "150_plus",
	}
	for move, want := range tests {
		if got := SwingMoveBucket(move); got != want {
			t.Fatalf("SwingMoveBucket(%v)=%q, want %q", move, got, want)
		}
	}
}

func TestReviewSwingsFindsThirtyPointMoves(t *testing.T) {
	bars := []marketmodel.Kline{
		{CloseTime: 1, High: "100", Low: "100", Close: "100"},
		{CloseTime: 2, High: "132", Low: "101", Close: "130"},
		{CloseTime: 3, High: "131", Low: "120", Close: "121"},
		{CloseTime: 4, High: "121", Low: "88", Close: "90"},
		{CloseTime: 5, High: "100", Low: "89", Close: "99"},
	}
	report, err := ReviewSwings(bars, nil, nil, nil, SwingReviewConfig{MinimumMovePoints: 30, ReversalPoints: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Opportunities) != 2 || report.UpSwings != 1 || report.DownSwings != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Opportunities[0].MovePoints != 32 || report.Opportunities[1].MovePoints != 44 {
		t.Fatalf("unexpected moves: %+v", report.Opportunities)
	}
	for _, opportunity := range report.Opportunities {
		if opportunity.MoveBucket != "30_60" {
			t.Fatalf("unexpected bucket: %+v", opportunity)
		}
	}
	marketSwings := BuildMarketSwings("binance", "um", "ETHUSDT", "3m", report)
	if len(marketSwings) != 2 || marketSwings[0].SwingID == "" || marketSwings[0].SwingID == marketSwings[1].SwingID {
		t.Fatalf("unexpected market swings: %+v", marketSwings)
	}
	second := BuildMarketSwings("binance", "um", "ETHUSDT", "3m", report)
	if marketSwings[0].SwingID != second[0].SwingID {
		t.Fatalf("market swing identity changed across builds: %q != %q", marketSwings[0].SwingID, second[0].SwingID)
	}
}

func TestReviewSwingsClassifiesEarlySignal(t *testing.T) {
	bars := []marketmodel.Kline{{CloseTime: 100, High: "100", Low: "100", Close: "100"}, {CloseTime: 200, High: "140", Low: "101", Close: "139"}, {CloseTime: 300, High: "139", Low: "125", Close: "126"}}
	report, err := ReviewSwings(bars, []SwingSignal{{TimeMS: 100, Side: "buy", Allowed: true}}, nil, nil, SwingReviewConfig{MinimumMovePoints: 30, ReversalPoints: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Opportunities) != 1 || report.Opportunities[0].HitStage != "early" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestMarketSwingsDoNotDependOnStrategySignals(t *testing.T) {
	bars := []marketmodel.Kline{
		{CloseTime: 100, High: "100", Low: "100", Close: "100"},
		{CloseTime: 200, High: "140", Low: "101", Close: "139"},
		{CloseTime: 300, High: "139", Low: "125", Close: "126"},
	}
	config := SwingReviewConfig{MinimumMovePoints: 30, ReversalPoints: 10}
	withoutStrategy, err := ReviewSwings(bars, nil, nil, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	withStrategy, err := ReviewSwings(bars, []SwingSignal{{TimeMS: 100, Side: "buy", Allowed: true}}, nil, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	base := BuildMarketSwings("binance", "um", "ETHUSDT", "3m", withoutStrategy)
	compared := BuildMarketSwings("binance", "um", "ETHUSDT", "3m", withStrategy)
	if len(base) != len(compared) || len(base) != 1 || base[0] != compared[0] {
		t.Fatalf("market swings changed with strategy input: base=%+v compared=%+v", base, compared)
	}
}

func TestReviewSwingsClassifiesContinuationEvidence(t *testing.T) {
	bars := []marketmodel.Kline{{CloseTime: 100, High: "100", Low: "100", Close: "100"}, {CloseTime: 200, High: "140", Low: "101", Close: "139"}, {CloseTime: 300, High: "139", Low: "125", Close: "126"}}
	report, err := ReviewSwings(bars, nil, []SwingEvidence{{TimeMS: 100, Side: "buy", Source: "ai_trend"}}, nil, SwingReviewConfig{MinimumMovePoints: 30, ReversalPoints: 10})
	if err != nil {
		t.Fatal(err)
	}
	if report.Opportunities[0].OpportunityType != "trend_continuation_missing" {
		t.Fatalf("unexpected opportunity: %+v", report.Opportunities[0])
	}
}

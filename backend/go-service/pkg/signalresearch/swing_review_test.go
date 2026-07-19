package signalresearch

import (
	"alphaflow/go-service/pkg/marketmodel"
	"testing"
)

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

package strategyframe

import (
	"reflect"
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestBuildContextProducesSharedTimeframes(t *testing.T) {
	target := strategy.Target{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m"}
	context, err := BuildContext(target, map[string]strategy.Snapshot{
		"3m": {Window: strategy.IndicatorWindowView{CloseTime: 1000}},
		"5m": {Window: strategy.IndicatorWindowView{CloseTime: 900}},
	}, 1000, strategy.TriggerOnEntryClose)
	if err != nil {
		t.Fatal(err)
	}
	entry := context.Snapshots["3m"]
	if entry.AsOf != 1000 || entry.Trigger != strategy.TriggerOnEntryClose {
		t.Fatalf("entry timing = %#v", entry)
	}
	if !reflect.DeepEqual(entry.Timeframes, context.Snapshots["5m"].Timeframes) {
		t.Fatal("snapshots do not share equivalent timeframe views")
	}
}

func TestBuildContextRejectsFutureWindow(t *testing.T) {
	_, err := BuildContext(strategy.Target{Interval: "3m"}, map[string]strategy.Snapshot{
		"3m": {Window: strategy.IndicatorWindowView{CloseTime: 1100}},
	}, 1000, strategy.TriggerOnEntryClose)
	if err == nil {
		t.Fatal("BuildContext() error = nil, want future window error")
	}
}

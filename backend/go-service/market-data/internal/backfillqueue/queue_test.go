package backfillqueue

import (
	"reflect"
	"testing"
)

func TestTaskRoundTripPreservesOptionalGapFields(t *testing.T) {
	task := DefaultTask()
	task.Exchange = "binance"
	task.Symbol = "ETHUSDT"
	task.Intervals = []string{"1m"}
	task.Start = "202607110100"
	task.End = "202607110105"
	task.Source = "collector_gap"
	task.Reason = "closed_kline_gap"
	payload, err := EncodeTask(task)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTask(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, task) {
		t.Fatalf("decoded task = %#v, want %#v", decoded, task)
	}
}

func TestTaskIDIgnoresReasonAndExecutionTuning(t *testing.T) {
	first := Task{Exchange: "binance", Symbol: "ETHUSDT", Intervals: []string{"5m", "1m"}, Start: "202607110100", End: "202607110105", Timezone: "UTC", Mode: "skip-existing", Source: "collector_gap", Reason: "first", Limit: 1000}
	second := first
	second.Reason = "second"
	second.Limit = 500
	second.Intervals = []string{"1M", " 5m "}
	if TaskID(first) != TaskID(second) {
		t.Fatalf("task ids differ: %q != %q", TaskID(first), TaskID(second))
	}
	second.End = "202607110106"
	if TaskID(first) == TaskID(second) {
		t.Fatal("task id did not change with range")
	}
	second = first
	second.Mode = "overwrite"
	if TaskID(first) == TaskID(second) {
		t.Fatal("task id did not change with mode")
	}
	second = first
	second.Timezone = "Asia/Shanghai"
	if TaskID(first) == TaskID(second) {
		t.Fatal("task id did not change with timezone")
	}
}

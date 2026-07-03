package admin

import (
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
)

func TestTimeRangeUsesMinutePrecisionAndRightOpenEnd(t *testing.T) {
	start, end, err := timeRange("202606010000", "202607010000", "Asia/Shanghai")
	if err != nil {
		t.Fatalf("timeRange: %v", err)
	}

	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	wantStart := time.Date(2026, 6, 1, 0, 0, 0, 0, location).UnixMilli()
	wantEnd := time.Date(2026, 7, 1, 0, 0, 0, 0, location).UnixMilli()
	if start != wantStart {
		t.Fatalf("start = %d, want %d", start, wantStart)
	}
	if end != wantEnd {
		t.Fatalf("end = %d, want %d", end, wantEnd)
	}
}

func TestTimeRangeRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		start string
		end   string
	}{
		{name: "short start", start: "20260601", end: "202607010000"},
		{name: "non digit", start: "20260601000x", end: "202607010000"},
		{name: "end before start", start: "202607010000", end: "202606010000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := timeRange(tt.start, tt.end, "Asia/Shanghai"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseListTrimsAndDeduplicates(t *testing.T) {
	got := parseList(" 1m,3m,1m,, 5m ")
	want := []string{"1m", "3m", "5m"}
	if len(got) != len(want) {
		t.Fatalf("parseList length = %d, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("parseList[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestExpectedKlines(t *testing.T) {
	const minute = int64(time.Minute / time.Millisecond)
	if got := expectedKlines(0, 60*minute, minute); got != 60 {
		t.Fatalf("expectedKlines 1m = %d, want 60", got)
	}
	if got := expectedKlines(0, 60*minute, 5*minute); got != 12 {
		t.Fatalf("expectedKlines 5m = %d, want 12", got)
	}
	if got := expectedKlines(60*minute, 0, minute); got != 0 {
		t.Fatalf("expectedKlines inverted = %d, want 0", got)
	}
}

func TestValidateCheckOptionsSupportsSingleAndMultipleIntervals(t *testing.T) {
	single := checkOptions{
		rangeOptions: rangeOptions{
			exchange: "binance",
			market:   "um",
			symbol:   "ETHUSDT",
			interval: "1m",
			start:    "202606010000",
			end:      "202607010000",
			timezone: "Asia/Shanghai",
		},
	}
	if err := validateCheckOptions(&single); err != nil {
		t.Fatalf("validate single interval: %v", err)
	}
	if len(single.intervals) != 1 || single.intervals[0] != "1m" {
		t.Fatalf("single intervals = %#v, want [1m]", single.intervals)
	}

	multiple := checkOptions{
		rangeOptions: rangeOptions{
			exchange: "binance",
			market:   "um",
			symbol:   "ETHUSDT",
			start:    "202606010000",
			end:      "202607010000",
			timezone: "Asia/Shanghai",
		},
		intervals: []string{"1m", "3m"},
	}
	if err := validateCheckOptions(&multiple); err != nil {
		t.Fatalf("validate multiple intervals: %v", err)
	}
}

func TestValidateCheckOptionsRejectsAmbiguousIntervals(t *testing.T) {
	opts := checkOptions{
		rangeOptions: rangeOptions{
			exchange: "binance",
			market:   "um",
			symbol:   "ETHUSDT",
			interval: "1m",
			start:    "202606010000",
			end:      "202607010000",
			timezone: "Asia/Shanghai",
		},
		intervals: []string{"3m"},
	}
	if err := validateCheckOptions(&opts); err == nil {
		t.Fatal("expected interval and intervals conflict")
	}
}

func TestValidateInventoryOptionsRequiresExactTargetForMissingIntervals(t *testing.T) {
	opts := inventoryOptions{
		exchange:  "binance",
		market:    "um",
		symbol:    "ETHUSDT",
		intervals: []string{"1m", "3m"},
		start:     "202606010000",
		end:       "202607010000",
		timezone:  "Asia/Shanghai",
	}
	if err := validateInventoryOptions(&opts); err != nil {
		t.Fatalf("validate inventory intervals: %v", err)
	}

	missingSymbol := opts
	missingSymbol.symbol = ""
	if err := validateInventoryOptions(&missingSymbol); err == nil {
		t.Fatal("expected missing symbol error")
	}
}

func TestSummarizeIntegrityReportsEveryMissingOpenTime(t *testing.T) {
	const minute = int64(time.Minute / time.Millisecond)
	existing := map[int64]struct{}{
		0:          {},
		2 * minute: {},
	}

	summary := summarizeIntegrity(existing, 0, 4*minute, minute)
	if summary.Expected != 4 {
		t.Fatalf("expected = %d, want 4", summary.Expected)
	}
	wantMissing := []int64{minute, 3 * minute}
	if len(summary.Missing) != len(wantMissing) {
		t.Fatalf("missing = %#v, want %#v", summary.Missing, wantMissing)
	}
	for index := range wantMissing {
		if summary.Missing[index] != wantMissing[index] {
			t.Fatalf("missing[%d] = %d, want %d", index, summary.Missing[index], wantMissing[index])
		}
	}
}

func TestPlanFetchJobsSkipsExistingAndSplitsByLimit(t *testing.T) {
	const minute = int64(time.Minute / time.Millisecond)
	existing := map[int64]struct{}{
		0:          {},
		3 * minute: {},
		4 * minute: {},
		8 * minute: {},
	}

	jobs := planFetchJobs(0, 10*minute, minute, existing, "skip-existing", 2)
	want := []fetchJob{
		{Start: minute, End: 3 * minute},
		{Start: 5 * minute, End: 7 * minute},
		{Start: 7 * minute, End: 8 * minute},
		{Start: 9 * minute, End: 10 * minute},
	}
	assertFetchJobs(t, jobs, want)
}

func TestPlanFetchJobsOverwriteFetchesWholeRange(t *testing.T) {
	const minute = int64(time.Minute / time.Millisecond)
	existing := map[int64]struct{}{
		0:      {},
		minute: {},
	}

	jobs := planFetchJobs(0, 5*minute, minute, existing, "overwrite", 2)
	want := []fetchJob{
		{Start: 0, End: 2 * minute},
		{Start: 2 * minute, End: 4 * minute},
		{Start: 4 * minute, End: 5 * minute},
	}
	assertFetchJobs(t, jobs, want)
}

func TestDedupeKlinesKeepsLastLogicalRow(t *testing.T) {
	klines := []marketmodel.Kline{
		testKline("binance", "um", "ETHUSDT", "1m", 60_000, "old"),
		testKline("binance", "um", "ETHUSDT", "1m", 0, "first"),
		testKline("binance", "um", "ETHUSDT", "1m", 60_000, "new"),
	}

	deduped := dedupeKlines(klines)
	if len(deduped) != 2 {
		t.Fatalf("deduped length = %d, want 2: %#v", len(deduped), deduped)
	}
	if deduped[0].OpenTime != 0 || deduped[0].Close != "first" {
		t.Fatalf("deduped[0] = %#v, want open_time 0 close first", deduped[0])
	}
	if deduped[1].OpenTime != 60_000 || deduped[1].Close != "new" {
		t.Fatalf("deduped[1] = %#v, want open_time 60000 close new", deduped[1])
	}
}

func TestValidateBackfillOptionsRejectsInvalidConcurrency(t *testing.T) {
	opts := validBackfillOptions()
	opts.concurrency = 0
	if err := validateBackfillOptions(opts); err == nil {
		t.Fatal("expected concurrency error")
	}
}

func TestDuplicateRowsCalculatesPhysicalMinusLogical(t *testing.T) {
	if got := duplicateRows(10, 13); got != 3 {
		t.Fatalf("duplicateRows = %d, want 3", got)
	}
	if got := duplicateRows(13, 10); got != 0 {
		t.Fatalf("duplicateRows inverted = %d, want 0", got)
	}
}

func TestValidateDuplicatesOptionsRejectsInvalidLimit(t *testing.T) {
	opts := duplicatesOptions{
		rangeOptions: rangeOptions{
			exchange: "binance",
			market:   "um",
			symbol:   "ETHUSDT",
			interval: "1m",
			start:    "202606010000",
			end:      "202607010000",
			timezone: "Asia/Shanghai",
		},
		limit: 0,
	}
	if err := validateDuplicatesOptions(opts); err == nil {
		t.Fatal("expected limit error")
	}
}

func TestValidateDuplicatesOptionsSupportsMultipleIntervals(t *testing.T) {
	opts := validDuplicatesOptions()
	opts.interval = ""
	opts.intervals = []string{"1m", "3m"}
	if err := validateDuplicatesOptions(opts); err != nil {
		t.Fatalf("validate duplicates intervals: %v", err)
	}
}

func TestValidateDuplicatesOptionsRejectsAmbiguousIntervals(t *testing.T) {
	opts := validDuplicatesOptions()
	opts.intervals = []string{"3m"}
	if err := validateDuplicatesOptions(opts); err == nil {
		t.Fatal("expected interval and intervals conflict")
	}
}

func TestValidateStatsOptionsSupportsMultipleIntervals(t *testing.T) {
	opts := validStatsOptions()
	opts.interval = ""
	opts.intervals = []string{"1m", "3m"}
	if err := validateStatsOptions(&opts); err != nil {
		t.Fatalf("validate stats intervals: %v", err)
	}
}

func TestValidateStatsOptionsRejectsAmbiguousIntervals(t *testing.T) {
	opts := validStatsOptions()
	opts.intervals = []string{"3m"}
	if err := validateStatsOptions(&opts); err == nil {
		t.Fatal("expected interval and intervals conflict")
	}
}

func TestDuplicateRatioFormatsPercentage(t *testing.T) {
	if got := duplicateRatio(1, 4); got != "25.00%" {
		t.Fatalf("duplicateRatio = %q, want 25.00%%", got)
	}
	if got := duplicateRatio(0, 0); got != "0.00%" {
		t.Fatalf("duplicateRatio zero = %q, want 0.00%%", got)
	}
}

func assertFetchJobs(t *testing.T, got []fetchJob, want []fetchJob) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("jobs length = %d, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("jobs[%d] = %#v, want %#v", index, got[index], want[index])
		}
	}
}

func validDuplicatesOptions() duplicatesOptions {
	return duplicatesOptions{
		rangeOptions: rangeOptions{
			exchange: "binance",
			market:   "um",
			symbol:   "ETHUSDT",
			interval: "1m",
			start:    "202606010000",
			end:      "202607010000",
			timezone: "Asia/Shanghai",
		},
		limit: 100,
	}
}

func validStatsOptions() statsOptions {
	return statsOptions{
		rangeOptions: rangeOptions{
			exchange: "binance",
			market:   "um",
			symbol:   "ETHUSDT",
			interval: "1m",
			start:    "202606010000",
			end:      "202607010000",
			timezone: "Asia/Shanghai",
		},
		maxMissingReport: 100,
	}
}

func validBackfillOptions() backfillOptions {
	return backfillOptions{
		exchange:         "binance",
		symbol:           "ETHUSDT",
		intervals:        []string{"1m"},
		start:            "202606010000",
		end:              "202607010000",
		timezone:         "Asia/Shanghai",
		mode:             "skip-existing",
		limit:            1000,
		batchSize:        1000,
		concurrency:      2,
		fetchRetries:     3,
		writeRetries:     3,
		retryDelay:       time.Second,
		maxMissingReport: 200,
	}
}

func testKline(
	exchange string,
	market string,
	symbol string,
	interval string,
	openTime int64,
	closeValue string,
) marketmodel.Kline {
	return marketmodel.Kline{
		Exchange:  exchange,
		Market:    market,
		Symbol:    symbol,
		Interval:  interval,
		OpenTime:  openTime,
		CloseTime: openTime + 59_999,
		Close:     closeValue,
		IsClosed:  true,
	}
}

package admin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/marketmodel"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
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

type fakeRedisStringGetter struct {
	values map[string]string
}

func (g fakeRedisStringGetter) Get(_ context.Context, key string) *redis.StringCmd {
	cmd := redis.NewStringCmd(context.Background())
	if value, ok := g.values[key]; ok {
		cmd.SetVal(value)
		return cmd
	}
	cmd.SetErr(redis.Nil)
	return cmd
}

func TestValidateMarketHealthOptions(t *testing.T) {
	opts := marketHealthOptions{
		exchange:  "binance",
		market:    "um",
		symbol:    "ETHUSDT",
		intervals: []string{"1m", "3m"},
	}
	if err := validateMarketHealthOptions(opts); err != nil {
		t.Fatalf("validateMarketHealthOptions() error = %v", err)
	}

	opts.intervals = []string{"bad"}
	if err := validateMarketHealthOptions(opts); err == nil {
		t.Fatal("validateMarketHealthOptions() error = nil, want invalid interval error")
	}
}

func TestReadMarketHealthRowsMarksMissingAndReady(t *testing.T) {
	health := model.DataHealth{
		Exchange:              "binance",
		Market:                "um",
		Symbol:                "ETHUSDT",
		Interval:              "1m",
		KlineStatus:           model.HealthStatusOK,
		IndicatorStatus:       model.HealthStatusOK,
		LastKlineOpenTime:     1000,
		LastIndicatorOpenTime: 1000,
		UpdatedAt:             1100,
	}
	payload, err := json.Marshal(health)
	if err != nil {
		t.Fatalf("marshal health: %v", err)
	}
	opts := marketHealthOptions{
		exchange:  "binance",
		market:    "um",
		symbol:    "ETHUSDT",
		intervals: []string{"1m", "3m"},
	}
	rows, err := readMarketHealthRows(context.Background(), fakeRedisStringGetter{
		values: map[string]string{
			model.DataHealthKey("binance", "um", "ETHUSDT", "1m"): string(payload),
		},
	}, opts)
	if err != nil {
		t.Fatalf("readMarketHealthRows() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if !rows[0].Ready {
		t.Fatalf("rows[0].Ready = false, want true")
	}
	if rows[1].Status != "missing" || rows[1].Ready {
		t.Fatalf("rows[1] = %#v, want missing and not ready", rows[1])
	}
	if marketHealthReady(rows) {
		t.Fatal("marketHealthReady() = true, want false with missing interval")
	}
}

func TestLoadTaskConfigRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.toml")
	content := `
exchange = "binance"
unknown = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write task config: %v", err)
	}

	if _, err := loadTaskConfig(path); err == nil {
		t.Fatal("loadTaskConfig() error = nil, want unknown field error")
	}
}

func TestApplyBackfillTaskConfigKeepsExplicitFlags(t *testing.T) {
	cmd := &cobra.Command{}
	opts := backfillOptions{}
	rawIntervals := "1m"
	cmd.Flags().StringVar(&opts.exchange, "exchange", "binance", "")
	cmd.Flags().StringVar(&opts.symbol, "symbol", "", "")
	cmd.Flags().StringVar(&rawIntervals, "intervals", "1m", "")
	cmd.Flags().StringVar(&opts.start, "start", "", "")
	cmd.Flags().StringVar(&opts.end, "end", "", "")
	cmd.Flags().StringVar(&opts.timezone, "timezone", "Asia/Shanghai", "")
	cmd.Flags().StringVar(&opts.mode, "mode", "skip-existing", "")
	cmd.Flags().IntVar(&opts.limit, "limit", 1000, "")
	cmd.Flags().IntVar(&opts.batchSize, "batch-size", 1000, "")
	cmd.Flags().IntVar(&opts.concurrency, "concurrency", 2, "")
	cmd.Flags().IntVar(&opts.fetchRetries, "fetch-retries", 3, "")
	cmd.Flags().IntVar(&opts.writeRetries, "write-retries", 3, "")
	cmd.Flags().DurationVar(&opts.retryDelay, "retry-delay", time.Second, "")
	cmd.Flags().IntVar(&opts.maxMissingReport, "max-missing-report", 200, "")
	cmd.Flags().Int64Var(&opts.warmupBars, "warmup-bars", 0, "")
	if err := cmd.Flags().Set("symbol", "BTCUSDT"); err != nil {
		t.Fatalf("set symbol: %v", err)
	}

	err := applyBackfillTaskConfig(cmd, taskConfig{
		Exchange:         "gate",
		Symbol:           "ETHUSDT",
		Intervals:        []string{"1m", "3m"},
		Start:            "202605010000",
		End:              "202607010000",
		Timezone:         "UTC",
		Mode:             "overwrite",
		Concurrency:      4,
		RetryDelay:       "2s",
		MaxMissingReport: 20,
		WarmupBars:       300,
	}, &opts, &rawIntervals)
	if err != nil {
		t.Fatalf("applyBackfillTaskConfig() error = %v", err)
	}
	if opts.exchange != "gate" {
		t.Fatalf("exchange = %q, want gate", opts.exchange)
	}
	if opts.symbol != "BTCUSDT" {
		t.Fatalf("symbol = %q, want explicit BTCUSDT", opts.symbol)
	}
	if rawIntervals != "1m,3m" {
		t.Fatalf("intervals = %q, want 1m,3m", rawIntervals)
	}
	if opts.concurrency != 4 {
		t.Fatalf("concurrency = %d, want 4", opts.concurrency)
	}
	if opts.retryDelay != 2*time.Second {
		t.Fatalf("retry delay = %s, want 2s", opts.retryDelay)
	}
	if opts.warmupBars != 300 {
		t.Fatalf("warmup bars = %d, want 300", opts.warmupBars)
	}
}

func TestBackfillTaskRoundTrip(t *testing.T) {
	opts := validBackfillOptions()
	opts.retryDelay = 2 * time.Second
	task := newBackfillTask(opts)
	payload, err := encodeBackfillTask(task)
	if err != nil {
		t.Fatalf("encodeBackfillTask() error = %v", err)
	}
	decoded, err := decodeBackfillTask(payload)
	if err != nil {
		t.Fatalf("decodeBackfillTask() error = %v", err)
	}
	decodedOptions, err := decoded.options()
	if err != nil {
		t.Fatalf("decoded options error = %v", err)
	}
	if decodedOptions.exchange != opts.exchange || decodedOptions.symbol != opts.symbol {
		t.Fatalf("decoded target = %s/%s, want %s/%s", decodedOptions.exchange, decodedOptions.symbol, opts.exchange, opts.symbol)
	}
	if decodedOptions.retryDelay != 2*time.Second {
		t.Fatalf("retry delay = %s, want 2s", decodedOptions.retryDelay)
	}
}

func TestNormalizeNATSBackfillTaskQueueOptionsDefaults(t *testing.T) {
	options := normalizeNATSBackfillTaskQueueOptions(natsBackfillTaskQueueOptions{})
	if options.Stream != defaultBackfillStream {
		t.Fatalf("stream = %q, want %q", options.Stream, defaultBackfillStream)
	}
	if options.Subject != "market.kline.backfill" {
		t.Fatalf("subject = %q, want market.kline.backfill", options.Subject)
	}
	if options.Durable != "market-data-backfill-worker" {
		t.Fatalf("durable = %q, want market-data-backfill-worker", options.Durable)
	}
	if options.AckWait != 30*time.Minute {
		t.Fatalf("ack wait = %s, want 30m", options.AckWait)
	}
	if options.MaxDeliveries != 3 {
		t.Fatalf("max deliveries = %d, want 3", options.MaxDeliveries)
	}
	if options.MaxPending != 10000 {
		t.Fatalf("max pending = %d, want 10000", options.MaxPending)
	}
	if options.DeadLetterSubject != "market.kline.backfill.dead" {
		t.Fatalf("dead letter subject = %q, want market.kline.backfill.dead", options.DeadLetterSubject)
	}
}

func TestProcessBackfillTaskMessageDeadLettersInvalidTask(t *testing.T) {
	queue := &fakeBackfillTaskQueue{}
	message := backfillTaskMessage{
		ID: "1",
		Task: backfillTask{
			Exchange:   "binance",
			Symbol:     "ETHUSDT",
			Intervals:  []string{"1m"},
			Start:      "202606010000",
			End:        "202607010000",
			Timezone:   "Asia/Shanghai",
			Mode:       "skip-existing",
			RetryDelay: "bad",
		},
		DeliveryCount: 1,
	}
	if err := processBackfillTaskMessage(context.Background(), "", queue, message, 3); err != nil {
		t.Fatalf("processBackfillTaskMessage() error = %v", err)
	}
	if len(queue.deadLetters) != 1 || queue.deadLetters[0].ID != "1" {
		t.Fatalf("dead letters = %#v, want message 1", queue.deadLetters)
	}
	if len(queue.acked) != 1 || queue.acked[0].ID != "1" {
		t.Fatalf("acked = %#v, want message 1", queue.acked)
	}
}

func TestProcessBackfillTaskMessageDeadLettersDecodeError(t *testing.T) {
	queue := &fakeBackfillTaskQueue{}
	message := backfillTaskMessage{
		ID:            "bad-json",
		DeliveryCount: 1,
		DecodeError:   "decode backfill task: invalid character",
		RawPayload:    []byte("{bad"),
	}
	if err := processBackfillTaskMessage(context.Background(), "", queue, message, 3); err != nil {
		t.Fatalf("processBackfillTaskMessage() error = %v", err)
	}
	if len(queue.deadLetters) != 1 || queue.deadLetters[0].ID != "bad-json" {
		t.Fatalf("dead letters = %#v, want message bad-json", queue.deadLetters)
	}
	if len(queue.acked) != 1 || queue.acked[0].ID != "bad-json" {
		t.Fatalf("acked = %#v, want message bad-json", queue.acked)
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

func TestValidateCheckOptionsRejectsNegativeWarmupBars(t *testing.T) {
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
		warmupBars: -1,
	}
	if err := validateCheckOptions(&opts); err == nil {
		t.Fatal("expected warmup-bars error")
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
	if summary.Actual != 2 {
		t.Fatalf("actual = %d, want 2", summary.Actual)
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

func TestIntegrityMissingDetailsSeparatesWarmupAndTrading(t *testing.T) {
	details := integrityMissingDetails([]int64{1000}, []int64{2000, 3000})
	want := []integrityMissingDetail{
		{Phase: "warmup", OpenTime: 1000},
		{Phase: "trading", OpenTime: 2000},
		{Phase: "trading", OpenTime: 3000},
	}
	if len(details) != len(want) {
		t.Fatalf("details length = %d, want %d: %#v", len(details), len(want), details)
	}
	for index := range want {
		if details[index] != want[index] {
			t.Fatalf("details[%d] = %#v, want %#v", index, details[index], want[index])
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

func TestWarmupStartUsesIntervalBars(t *testing.T) {
	const hour = int64(time.Hour / time.Millisecond)

	got, err := warmupStart(1000*hour, "4h", 300)
	if err != nil {
		t.Fatalf("warmupStart: %v", err)
	}
	want := int64(1000-4*300) * hour
	if got != want {
		t.Fatalf("warmup start = %d, want %d", got, want)
	}
}

func TestEffectiveWarmupRangeKeepsRequestedStart(t *testing.T) {
	const minute = int64(time.Minute / time.Millisecond)

	got, err := effectiveWarmupRange(1000*minute, 2000*minute, "5m", 300)
	if err != nil {
		t.Fatalf("effectiveWarmupRange: %v", err)
	}
	if got.RequestedStart != 1000*minute {
		t.Fatalf("requested start = %d, want %d", got.RequestedStart, 1000*minute)
	}
	if got.EffectiveStart != (1000-5*300)*minute {
		t.Fatalf("effective start = %d, want %d", got.EffectiveStart, (1000-5*300)*minute)
	}
	if got.End != 2000*minute {
		t.Fatalf("end = %d, want %d", got.End, 2000*minute)
	}
	if got.WarmupBars != 300 {
		t.Fatalf("warmup bars = %d, want 300", got.WarmupBars)
	}
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

func TestValidateBackfillOptionsRejectsNegativeWarmupBars(t *testing.T) {
	opts := validBackfillOptions()
	opts.warmupBars = -1
	if err := validateBackfillOptions(opts); err == nil {
		t.Fatal("expected warmup-bars error")
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
		warmupBars:       0,
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

type fakeBackfillTaskQueue struct {
	published   []backfillTask
	messages    []backfillTaskMessage
	acked       []backfillTaskMessage
	deadLetters []backfillTaskMessage
}

func (q *fakeBackfillTaskQueue) Publish(ctx context.Context, task backfillTask) (string, error) {
	q.published = append(q.published, task)
	return "1", ctx.Err()
}

func (q *fakeBackfillTaskQueue) Fetch(ctx context.Context, batch int, maxWait time.Duration) ([]backfillTaskMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(q.messages) == 0 {
		return nil, nil
	}
	count := len(q.messages)
	if batch > 0 && count > batch {
		count = batch
	}
	messages := append([]backfillTaskMessage(nil), q.messages[:count]...)
	q.messages = q.messages[count:]
	return messages, nil
}

func (q *fakeBackfillTaskQueue) Ack(ctx context.Context, messages []backfillTaskMessage) error {
	q.acked = append(q.acked, messages...)
	return ctx.Err()
}

func (q *fakeBackfillTaskQueue) DeadLetter(ctx context.Context, message backfillTaskMessage, reason string) error {
	q.deadLetters = append(q.deadLetters, message)
	return ctx.Err()
}

func (q *fakeBackfillTaskQueue) Close() error {
	return nil
}

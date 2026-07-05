package admin

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"alphaflow/go-service/market-data/internal/aggregator"
	"alphaflow/go-service/market-data/internal/config"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/marketmodel"
	"github.com/spf13/cobra"
)

type backfillOptions struct {
	exchange         string
	symbol           string
	intervals        []string
	start            string
	end              string
	timezone         string
	mode             string
	limit            int
	batchSize        int
	concurrency      int
	fetchRetries     int
	writeRetries     int
	retryDelay       time.Duration
	maxMissingReport int
	warmupBars       int64
	async            bool
}

type fetchJob struct {
	Start int64
	End   int64
}

type fetchJobResult struct {
	Klines          []marketmodel.Kline
	Fetched         int
	SkippedExisting int
	Err             error
}

type klineKey struct {
	Exchange string
	Market   string
	Symbol   string
	Interval string
	OpenTime int64
}

func newBackfillCommand(ctx context.Context, root *rootOptions) *cobra.Command {
	opts := backfillOptions{}
	var rawIntervals string
	var taskConfigPath string
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Backfill missing historical klines into ClickHouse and verify completeness",
		Long: strings.TrimSpace(`
Backfill historical K lines into ClickHouse, then verify that every requested
interval is complete.

By default, mode is skip-existing:
  - existing open_time rows are skipped
  - only missing K lines are written
  - the command is safe to rerun for the same range

Use --mode overwrite to write fetched rows again. ClickHouse uses
ReplacingMergeTree, so repeated logical rows are eventually collapsed, but
skip-existing is preferred for normal maintenance.

Time ranges use kline open_time with left-closed, right-open semantics:

  start <= open_time < end

Native exchange intervals are fetched through REST. Missing exchange intervals
that AlphaFlow supports are generated from smaller source intervals. For
Binance, 10m is generated from complete 5m data.

For native REST intervals, skip-existing mode plans requests from ClickHouse
missing open_time ranges instead of scanning the full range. Use --concurrency
to control parallel REST requests.

The command exits with a non-zero status if any requested interval is still
missing K lines after backfill.
`),
		Example: "market-data-admin backfill --exchange binance --symbol ETHUSDT --intervals 1m,3m,5m,15m,30m,1h,2h,4h --start 202606010000 --end 202607010000 --warmup-bars 300\n" +
			"market-data-admin backfill --task-config configs/tasks/kline-default.toml\n" +
			"market-data-admin backfill --exchange binance --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202606010000 --end 202607010000 --limit 1000\n" +
			"market-data-admin backfill --exchange binance --symbol ETHUSDT --intervals 10m --start 202606010000 --end 202607010000",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskCfg, err := loadTaskConfig(taskConfigPath)
			if err != nil {
				return err
			}
			if err := applyBackfillTaskConfig(cmd, taskCfg, &opts, &rawIntervals); err != nil {
				return err
			}
			opts.exchange = strings.ToLower(strings.TrimSpace(opts.exchange))
			opts.symbol = strings.ToUpper(strings.TrimSpace(opts.symbol))
			opts.intervals = parseList(rawIntervals)
			opts.timezone = strings.TrimSpace(opts.timezone)
			opts.mode = strings.ToLower(strings.TrimSpace(opts.mode))
			if opts.async {
				return enqueueBackfill(ctx, root.configPath, opts)
			}
			return runBackfill(ctx, root.configPath, opts)
		},
	}
	cmd.Flags().StringVar(&opts.exchange, "exchange", "binance", "exchange: binance, gate, bitget, bybit")
	cmd.Flags().StringVar(&opts.symbol, "symbol", "", "symbol to backfill, for example ETHUSDT or ETH_USDT")
	cmd.Flags().StringVar(&rawIntervals, "intervals", "1m", "comma-separated intervals, for example 1m,5m,1h")
	cmd.Flags().StringVar(&opts.start, "start", "", "inclusive start time in YYYYMMDDHHmm")
	cmd.Flags().StringVar(&opts.end, "end", "", "exclusive end time in YYYYMMDDHHmm")
	cmd.Flags().StringVar(&opts.timezone, "timezone", "Asia/Shanghai", "date boundary timezone")
	cmd.Flags().StringVar(&opts.mode, "mode", "skip-existing", "backfill mode: skip-existing or overwrite")
	cmd.Flags().IntVar(&opts.limit, "limit", config.RESTLimit(), "REST kline page size")
	cmd.Flags().IntVar(&opts.batchSize, "batch-size", 1000, "ClickHouse write batch size")
	cmd.Flags().IntVar(&opts.concurrency, "concurrency", 2, "maximum concurrent REST fetch requests per interval")
	cmd.Flags().IntVar(&opts.fetchRetries, "fetch-retries", 3, "REST fetch retries after the first attempt")
	cmd.Flags().IntVar(&opts.writeRetries, "write-retries", 3, "ClickHouse write retries after the first attempt")
	cmd.Flags().DurationVar(&opts.retryDelay, "retry-delay", time.Second, "base retry delay")
	cmd.Flags().IntVar(&opts.maxMissingReport, "max-missing-report", 200, "maximum missing klines to log per interval")
	cmd.Flags().Int64Var(&opts.warmupBars, "warmup-bars", 0, "extra kline bars to fetch before start for indicator warm-up")
	cmd.Flags().BoolVar(&opts.async, "async", false, "submit backfill task to NATS JetStream and return without running it")
	addTaskConfigFlag(cmd, &taskConfigPath)
	return cmd
}

func enqueueBackfill(ctx context.Context, configPath string, opts backfillOptions) error {
	if err := validateBackfillOptions(opts); err != nil {
		return err
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	queue, err := newNATSBackfillTaskQueue(cfg)
	if err != nil {
		return err
	}
	defer queue.Close()
	messageID, err := queue.Publish(ctx, newBackfillTask(opts))
	if err != nil {
		return err
	}
	slog.Info(
		"submitted backfill task",
		"message_id", messageID,
		"exchange", opts.exchange,
		"symbol", opts.symbol,
		"intervals", opts.intervals,
		"start", opts.start,
		"end", opts.end,
	)
	return nil
}

func runBackfill(ctx context.Context, configPath string, opts backfillOptions) error {
	if err := validateBackfillOptions(opts); err != nil {
		return err
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	start, end, err := timeRange(opts.start, opts.end, opts.timezone)
	if err != nil {
		return err
	}

	adminStore, err := newAdminStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer adminStore.Close()

	writeStore, err := newClickHouseWriteStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer writeStore.Close()

	client, err := newRESTClient(opts.exchange)
	if err != nil {
		return err
	}

	nativeIntervals, derivedIntervals, err := splitBackfillIntervals(client.Exchange(), opts.intervals)
	if err != nil {
		return err
	}
	for _, interval := range nativeIntervals {
		backfillRange, err := effectiveWarmupRange(start, end, interval, opts.warmupBars)
		if err != nil {
			return err
		}
		if err := backfillRESTInterval(ctx, adminStore, writeStore, client, opts, interval, backfillRange); err != nil {
			return err
		}
	}
	for _, rule := range derivedIntervals {
		backfillRange, err := effectiveWarmupRange(start, end, rule.TargetInterval, opts.warmupBars)
		if err != nil {
			return err
		}
		if err := backfillDerivedInterval(ctx, adminStore, writeStore, client, opts, rule, backfillRange); err != nil {
			return err
		}
	}
	return nil
}

func validateBackfillOptions(opts backfillOptions) error {
	if opts.symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if len(opts.intervals) == 0 {
		return fmt.Errorf("intervals is required")
	}
	if opts.start == "" {
		return fmt.Errorf("start is required")
	}
	if opts.end == "" {
		return fmt.Errorf("end is required")
	}
	if opts.limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	if opts.batchSize <= 0 {
		return fmt.Errorf("batch-size must be positive")
	}
	if opts.concurrency <= 0 {
		return fmt.Errorf("concurrency must be positive")
	}
	if opts.mode != "skip-existing" && opts.mode != "overwrite" {
		return fmt.Errorf("unsupported mode %q", opts.mode)
	}
	if opts.fetchRetries < 0 {
		return fmt.Errorf("fetch-retries cannot be negative")
	}
	if opts.writeRetries < 0 {
		return fmt.Errorf("write-retries cannot be negative")
	}
	if opts.retryDelay <= 0 {
		return fmt.Errorf("retry-delay must be positive")
	}
	if opts.maxMissingReport < 0 {
		return fmt.Errorf("max-missing-report cannot be negative")
	}
	if err := validateWarmupBars(opts.warmupBars); err != nil {
		return err
	}
	for _, interval := range opts.intervals {
		if _, err := marketmodel.IntervalMillis(interval); err != nil {
			return err
		}
	}
	if _, _, err := timeRange(opts.start, opts.end, opts.timezone); err != nil {
		return err
	}
	return nil
}

func newClickHouseWriteStore(ctx context.Context, cfg config.Config) (*clickhousemarket.Store, error) {
	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return nil, err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return nil, err
	}
	store, err := clickhousemarket.NewStore(ctx, clickhousemarket.Options{
		Addr:        cfg.ClickHouse.Addr,
		Database:    cfg.ClickHouse.Database,
		Username:    cfg.ClickHouse.Username,
		Password:    cfg.ClickHouse.Password,
		DialTimeout: dialTimeout,
		ReadTimeout: readTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("connect clickhouse: %w", err)
	}
	return store, nil
}

func splitBackfillIntervals(exchange string, intervals []string) ([]string, []aggregator.Rule, error) {
	native := []string{}
	derived := []aggregator.Rule{}
	for _, interval := range intervals {
		if isNativeInterval(exchange, interval) {
			native = append(native, interval)
			continue
		}
		rule, ok := derivedRule(exchange, interval)
		if !ok {
			return nil, nil, fmt.Errorf("unsupported interval %q for exchange %s", interval, exchange)
		}
		derived = append(derived, rule)
	}
	return native, derived, nil
}

func isNativeInterval(exchange string, interval string) bool {
	for _, item := range nativeIntervals(exchange) {
		if item == interval {
			return true
		}
	}
	return false
}

func nativeIntervals(exchange string) []string {
	switch exchange {
	case "binance":
		return config.BinanceIntervals()
	case "gate":
		return config.GateIntervals()
	case "bitget":
		return config.BitgetIntervals()
	case "bybit":
		return config.BybitIntervals()
	default:
		return nil
	}
}

func derivedRule(exchange string, interval string) (aggregator.Rule, bool) {
	switch exchange {
	case "binance":
		if interval == "10m" {
			return aggregator.Rule{Exchange: "binance", Market: "um", SourceInterval: "5m", TargetInterval: "10m"}, true
		}
	case "gate":
		switch interval {
		case "3m":
			return aggregator.Rule{Exchange: "gate", Market: config.GateSettle(), SourceInterval: "1m", TargetInterval: "3m"}, true
		case "10m":
			return aggregator.Rule{Exchange: "gate", Market: config.GateSettle(), SourceInterval: "5m", TargetInterval: "10m"}, true
		case "2h":
			return aggregator.Rule{Exchange: "gate", Market: config.GateSettle(), SourceInterval: "1h", TargetInterval: "2h"}, true
		}
	case "bitget":
		switch interval {
		case "3m":
			return aggregator.Rule{Exchange: "bitget", Market: strings.ToLower(config.BitgetProductType()), SourceInterval: "1m", TargetInterval: "3m"}, true
		case "10m":
			return aggregator.Rule{Exchange: "bitget", Market: strings.ToLower(config.BitgetProductType()), SourceInterval: "5m", TargetInterval: "10m"}, true
		case "2h":
			return aggregator.Rule{Exchange: "bitget", Market: strings.ToLower(config.BitgetProductType()), SourceInterval: "1h", TargetInterval: "2h"}, true
		}
	case "bybit":
		if interval == "10m" {
			return aggregator.Rule{Exchange: "bybit", Market: config.BybitCategory(), SourceInterval: "5m", TargetInterval: "10m"}, true
		}
	}
	return aggregator.Rule{}, false
}

func backfillRESTInterval(
	ctx context.Context,
	adminStore *adminStore,
	writeStore *clickhousemarket.Store,
	client restClient,
	opts backfillOptions,
	interval string,
	backfillRange warmupRange,
) error {
	intervalMillis, err := marketmodel.IntervalMillis(interval)
	if err != nil {
		return err
	}

	existingOpenTimes := map[int64]struct{}{}
	if opts.mode == "skip-existing" {
		existingOpenTimes, err = adminStore.ExistingOpenTimes(ctx, client.Exchange(), client.Market(), opts.symbol, interval, backfillRange.EffectiveStart, backfillRange.End)
		if err != nil {
			return err
		}
	}
	initialExisting := len(existingOpenTimes)

	now := time.Now().UnixMilli()
	jobs := planFetchJobs(backfillRange.EffectiveStart, backfillRange.End, intervalMillis, existingOpenTimes, opts.mode, opts.limit)
	filtered, totalFetched, totalSkippedExisting, err := fetchKlineJobs(
		ctx,
		client,
		opts,
		interval,
		jobs,
		now,
		existingOpenTimes,
	)
	if err != nil {
		return fmt.Errorf("fetch %s %s %s klines: %w", client.Exchange(), opts.symbol, interval, err)
	}

	sortKlines(filtered)
	written, err := writeBatches(ctx, writeStore, filtered, opts.batchSize, opts.writeRetries, opts.retryDelay)
	if err != nil {
		return fmt.Errorf("write %s %s %s klines: %w", client.Exchange(), opts.symbol, interval, err)
	}

	slog.Info(
		"backfilled klines",
		"exchange", client.Exchange(),
		"market", client.Market(),
		"symbol", opts.symbol,
		"interval", interval,
		"mode", opts.mode,
		"initial_existing", initialExisting,
		"fetch_jobs", len(jobs),
		"fetched", totalFetched,
		"skipped_existing", totalSkippedExisting,
		"written", written,
		"requested_start", backfillRange.RequestedStart,
		"effective_start", backfillRange.EffectiveStart,
		"end_exclusive", backfillRange.End,
		"warmup_bars", backfillRange.WarmupBars,
	)

	return checkIntegrity(ctx, adminStore, client.Exchange(), client.Market(), opts.symbol, interval, backfillRange.EffectiveStart, backfillRange.End, intervalMillis, opts.timezone, opts.maxMissingReport, true, backfillRange)
}

func backfillDerivedInterval(
	ctx context.Context,
	adminStore *adminStore,
	writeStore *clickhousemarket.Store,
	client restClient,
	opts backfillOptions,
	rule aggregator.Rule,
	backfillRange warmupRange,
) error {
	sourceMillis, err := marketmodel.IntervalMillis(rule.SourceInterval)
	if err != nil {
		return err
	}
	targetMillis, err := marketmodel.IntervalMillis(rule.TargetInterval)
	if err != nil {
		return err
	}
	if targetMillis%sourceMillis != 0 {
		return fmt.Errorf("target interval %s is not divisible by source interval %s", rule.TargetInterval, rule.SourceInterval)
	}

	existingOpenTimes := map[int64]struct{}{}
	if opts.mode == "skip-existing" {
		existingOpenTimes, err = adminStore.ExistingOpenTimes(ctx, rule.Exchange, rule.Market, opts.symbol, rule.TargetInterval, backfillRange.EffectiveStart, backfillRange.End)
		if err != nil {
			return err
		}
	}
	initialExisting := len(existingOpenTimes)
	sourceKlines, err := writeStore.RangeKlines(ctx, rule.Exchange, rule.Market, opts.symbol, rule.SourceInterval, backfillRange.EffectiveStart, backfillRange.End-1)
	if err != nil {
		return fmt.Errorf("query source klines %s %s %s range: %w", rule.Exchange, opts.symbol, rule.SourceInterval, err)
	}
	sourceByOpenTime := make(map[int64]marketmodel.Kline, len(sourceKlines))
	for _, kline := range sourceKlines {
		sourceByOpenTime[kline.OpenTime] = kline
	}

	pendingWrites := make([]marketmodel.Kline, 0)
	totalWritten := 0
	totalSkippedExisting := 0
	totalSkippedMissingSource := 0
	for openTime := backfillRange.EffectiveStart; openTime < backfillRange.End; openTime += targetMillis {
		if _, ok := existingOpenTimes[openTime]; ok {
			totalSkippedExisting++
			continue
		}
		window := make([]marketmodel.Kline, 0, int(targetMillis/sourceMillis))
		for sourceOpenTime := openTime; sourceOpenTime < openTime+targetMillis; sourceOpenTime += sourceMillis {
			kline, ok := sourceByOpenTime[sourceOpenTime]
			if !ok {
				break
			}
			window = append(window, kline)
		}
		aggregated, ok, err := aggregator.Aggregate(rule, opts.symbol, openTime, window)
		if err != nil {
			return fmt.Errorf("aggregate %s %s %s %s->%s open_time=%d: %w",
				rule.Exchange,
				rule.Market,
				opts.symbol,
				rule.SourceInterval,
				rule.TargetInterval,
				openTime,
				err,
			)
		}
		if !ok {
			totalSkippedMissingSource++
			continue
		}
		pendingWrites = append(pendingWrites, aggregated)
		if len(pendingWrites) >= opts.batchSize {
			written, err := writeBatches(ctx, writeStore, pendingWrites, opts.batchSize, opts.writeRetries, opts.retryDelay)
			if err != nil {
				return fmt.Errorf("write derived %s %s %s kline: %w", rule.Exchange, opts.symbol, rule.TargetInterval, err)
			}
			totalWritten += written
			pendingWrites = pendingWrites[:0]
		}
		existingOpenTimes[openTime] = struct{}{}
	}
	if len(pendingWrites) > 0 {
		written, err := writeBatches(ctx, writeStore, pendingWrites, opts.batchSize, opts.writeRetries, opts.retryDelay)
		if err != nil {
			return fmt.Errorf("write derived %s %s %s kline: %w", rule.Exchange, opts.symbol, rule.TargetInterval, err)
		}
		totalWritten += written
	}

	slog.Info(
		"derived backfilled klines",
		"exchange", rule.Exchange,
		"market", rule.Market,
		"symbol", opts.symbol,
		"source_interval", rule.SourceInterval,
		"target_interval", rule.TargetInterval,
		"mode", opts.mode,
		"initial_existing", initialExisting,
		"source", len(sourceKlines),
		"skipped_existing", totalSkippedExisting,
		"skipped_missing_source", totalSkippedMissingSource,
		"written", totalWritten,
		"requested_start", backfillRange.RequestedStart,
		"effective_start", backfillRange.EffectiveStart,
		"end_exclusive", backfillRange.End,
		"warmup_bars", backfillRange.WarmupBars,
	)
	return checkIntegrity(ctx, adminStore, rule.Exchange, rule.Market, opts.symbol, rule.TargetInterval, backfillRange.EffectiveStart, backfillRange.End, targetMillis, opts.timezone, opts.maxMissingReport, true, backfillRange)
}

func planFetchJobs(
	start int64,
	end int64,
	intervalMillis int64,
	existingOpenTimes map[int64]struct{},
	mode string,
	limit int,
) []fetchJob {
	if start >= end || intervalMillis <= 0 || limit <= 0 {
		return nil
	}
	if mode == "overwrite" {
		return splitFetchRange(start, end, intervalMillis, limit)
	}

	jobs := []fetchJob{}
	var rangeStart int64
	inRange := false
	for openTime := start; openTime < end; openTime += intervalMillis {
		if _, ok := existingOpenTimes[openTime]; ok {
			if inRange {
				jobs = append(jobs, splitFetchRange(rangeStart, openTime, intervalMillis, limit)...)
				inRange = false
			}
			continue
		}
		if !inRange {
			rangeStart = openTime
			inRange = true
		}
	}
	if inRange {
		jobs = append(jobs, splitFetchRange(rangeStart, end, intervalMillis, limit)...)
	}
	return jobs
}

func splitFetchRange(start int64, end int64, intervalMillis int64, limit int) []fetchJob {
	maxSpan := int64(limit) * intervalMillis
	if maxSpan <= 0 {
		return nil
	}
	jobs := []fetchJob{}
	for cursor := start; cursor < end; cursor += maxSpan {
		jobEnd := cursor + maxSpan
		if jobEnd > end {
			jobEnd = end
		}
		jobs = append(jobs, fetchJob{Start: cursor, End: jobEnd})
	}
	return jobs
}

func fetchKlineJobs(
	ctx context.Context,
	client restClient,
	opts backfillOptions,
	interval string,
	jobs []fetchJob,
	now int64,
	existingOpenTimes map[int64]struct{},
) ([]marketmodel.Kline, int, int, error) {
	if len(jobs) == 0 {
		return nil, 0, 0, nil
	}

	workerCount := opts.concurrency
	if len(jobs) < workerCount {
		workerCount = len(jobs)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobCh := make(chan fetchJob)
	resultCh := make(chan fetchJobResult, len(jobs))
	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				if ctx.Err() != nil {
					return
				}
				klines, err := fetchKlinesWithRetry(ctx, client, opts.symbol, interval, opts.limit, job.Start, opts.fetchRetries, opts.retryDelay)
				if err != nil {
					resultCh <- fetchJobResult{Err: fmt.Errorf("start=%d end=%d: %w", job.Start, job.End, err)}
					cancel()
					return
				}
				filtered, skippedExisting := filterKlines(klines, job.Start, job.End, now, existingOpenTimes)
				resultCh <- fetchJobResult{
					Klines:          filtered,
					Fetched:         len(klines),
					SkippedExisting: skippedExisting,
				}
			}
		}()
	}

	go func() {
		defer close(jobCh)
		for _, job := range jobs {
			select {
			case <-ctx.Done():
				return
			case jobCh <- job:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	klines := []marketmodel.Kline{}
	totalFetched := 0
	totalSkippedExisting := 0
	for result := range resultCh {
		if result.Err != nil {
			return nil, totalFetched, totalSkippedExisting, result.Err
		}
		totalFetched += result.Fetched
		totalSkippedExisting += result.SkippedExisting
		klines = append(klines, result.Klines...)
	}
	return klines, totalFetched, totalSkippedExisting, nil
}

func fetchKlinesWithRetry(
	ctx context.Context,
	client restClient,
	symbol string,
	interval string,
	limit int,
	startTime int64,
	retries int,
	retryDelay time.Duration,
) ([]marketmodel.Kline, error) {
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, retryDelay*time.Duration(attempt)); err != nil {
				return nil, err
			}
		}
		klines, err := client.FetchKlines(ctx, symbol, interval, limit, startTime)
		if err == nil {
			return klines, nil
		}
		lastErr = err
		slog.Warn(
			"fetch retry",
			"exchange", client.Exchange(),
			"symbol", symbol,
			"interval", interval,
			"start", startTime,
			"attempt", attempt+1,
			"error", err,
		)
	}
	return nil, lastErr
}

func filterKlines(
	klines []marketmodel.Kline,
	start int64,
	end int64,
	now int64,
	existingOpenTimes map[int64]struct{},
) ([]marketmodel.Kline, int) {
	filtered := make([]marketmodel.Kline, 0, len(klines))
	skippedExisting := 0
	for _, kline := range klines {
		if kline.OpenTime < start || kline.OpenTime >= end {
			continue
		}
		if kline.CloseTime >= now {
			continue
		}
		if _, ok := existingOpenTimes[kline.OpenTime]; ok {
			skippedExisting++
			continue
		}
		filtered = append(filtered, kline)
	}
	return filtered, skippedExisting
}

func writeBatches(
	ctx context.Context,
	store *clickhousemarket.Store,
	klines []marketmodel.Kline,
	batchSize int,
	retries int,
	retryDelay time.Duration,
) (int, error) {
	klines = dedupeKlines(klines)
	written := 0
	for len(klines) > 0 {
		size := batchSize
		if len(klines) < size {
			size = len(klines)
		}
		if err := writeKlinesWithRetry(ctx, store, klines[:size], retries, retryDelay); err != nil {
			return written, err
		}
		written += size
		klines = klines[size:]
	}
	return written, nil
}

func dedupeKlines(klines []marketmodel.Kline) []marketmodel.Kline {
	if len(klines) < 2 {
		return klines
	}
	byKey := make(map[klineKey]marketmodel.Kline, len(klines))
	for _, kline := range klines {
		byKey[klineLogicalKey(kline)] = kline
	}
	if len(byKey) == len(klines) {
		return klines
	}
	deduped := make([]marketmodel.Kline, 0, len(byKey))
	for _, kline := range byKey {
		deduped = append(deduped, kline)
	}
	sortKlines(deduped)
	return deduped
}

func klineLogicalKey(kline marketmodel.Kline) klineKey {
	return klineKey{
		Exchange: kline.Exchange,
		Market:   kline.Market,
		Symbol:   kline.Symbol,
		Interval: kline.Interval,
		OpenTime: kline.OpenTime,
	}
}

func sortKlines(klines []marketmodel.Kline) {
	sort.Slice(klines, func(i int, j int) bool {
		left := klineLogicalKey(klines[i])
		right := klineLogicalKey(klines[j])
		if left.Exchange != right.Exchange {
			return left.Exchange < right.Exchange
		}
		if left.Market != right.Market {
			return left.Market < right.Market
		}
		if left.Symbol != right.Symbol {
			return left.Symbol < right.Symbol
		}
		if left.Interval != right.Interval {
			return left.Interval < right.Interval
		}
		return left.OpenTime < right.OpenTime
	})
}

func writeKlinesWithRetry(
	ctx context.Context,
	store *clickhousemarket.Store,
	klines []marketmodel.Kline,
	retries int,
	retryDelay time.Duration,
) error {
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, retryDelay*time.Duration(attempt)); err != nil {
				return err
			}
		}
		if err := store.WriteKlines(ctx, klines); err != nil {
			lastErr = err
			slog.Warn(
				"write retry",
				"batch_size", len(klines),
				"attempt", attempt+1,
				"error", err,
			)
			continue
		}
		return nil
	}
	return lastErr
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

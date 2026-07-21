package collector

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/aggregator"
	"alphaflow/go-service/market-data/internal/backfillqueue"
	"alphaflow/go-service/market-data/internal/exchange"
	"alphaflow/go-service/market-data/internal/model"
)

type fakeStore struct {
	lastOpenTime      int64
	hasLast           bool
	mu                sync.Mutex
	statuses          []model.MarketStatus
	wsStatuses        []model.WebSocketStatus
	klines            int64
	lastPrices        int64
	lastPriceBySymbol map[string]model.LastPrice
	lastPriceBlock    <-chan struct{}
	bookTickers       int64
	rangeKlines       []model.Kline
	upsertKlineErrors []error
}

type fakeGapPublisher struct {
	tasks []backfillqueue.Task
	err   error
}

func (p *fakeGapPublisher) Publish(ctx context.Context, task backfillqueue.Task) (string, error) {
	p.tasks = append(p.tasks, task)
	return "gap-1", p.err
}

type fakeREST struct {
	fetchKlinesErr         error
	fetchKlinesErrBySymbol map[string]error
	fetchKlines            int
	fetchTimes             []time.Time
	fetchRequests          []backfillRequest
	openInterestSymbols    []string
	openInterestErr        error
	klines                 []model.Kline
}

type backfillRequest struct {
	symbol   string
	interval string
	limit    int
	start    int64
}

type fakeWS struct {
	runs      chan []exchange.Stream
	remaining int64
	cancel    context.CancelFunc
}

func (fakeREST) Exchange() string {
	return "binance"
}

func (fakeREST) Market() string {
	return "um"
}

func (r *fakeREST) FetchKlines(
	_ context.Context,
	symbol string,
	interval string,
	limit int,
	start int64,
) ([]model.Kline, error) {
	r.fetchKlines++
	r.fetchTimes = append(r.fetchTimes, time.Now())
	r.fetchRequests = append(r.fetchRequests, backfillRequest{symbol: symbol, interval: interval, limit: limit, start: start})
	if err := r.fetchKlinesErrBySymbol[symbol]; err != nil {
		return nil, err
	}
	if r.fetchKlinesErr != nil {
		return nil, r.fetchKlinesErr
	}
	if r.klines != nil {
		return append([]model.Kline(nil), r.klines...), nil
	}
	intervalMillis, _ := model.IntervalMillis(interval)
	openTime := start
	return []model.Kline{{
		Exchange:  "binance",
		Market:    "um",
		Symbol:    "ETHUSDT",
		Interval:  interval,
		OpenTime:  openTime,
		CloseTime: openTime + intervalMillis - 1,
		IsClosed:  true,
	}}, nil
}

func (r *fakeREST) FetchOpenInterest(_ context.Context, symbol string) (model.OpenInterest, error) {
	r.openInterestSymbols = append(r.openInterestSymbols, symbol)
	if r.openInterestErr != nil {
		return model.OpenInterest{}, r.openInterestErr
	}
	return model.OpenInterest{
		Exchange:     "binance",
		Market:       "um",
		Symbol:       symbol,
		OpenInterest: "100",
		Time:         time.Now().UnixMilli(),
	}, nil
}

func (w *fakeWS) Run(ctx context.Context, streams []exchange.Stream, _ exchange.Handler) error {
	w.runs <- streams
	if atomic.AddInt64(&w.remaining, -1) == 0 {
		w.cancel()
	}
	<-ctx.Done()
	return ctx.Err()
}

func (s *fakeStore) LastOpenTime(
	context.Context,
	string,
	string,
	string,
	string,
) (int64, bool, error) {
	return s.lastOpenTime, s.hasLast, nil
}

func (s *fakeStore) RangeKlines(context.Context, string, string, string, string, int64, int64) ([]model.Kline, error) {
	return append([]model.Kline(nil), s.rangeKlines...), nil
}

func (s *fakeStore) UpsertKline(context.Context, model.Kline) error {
	s.mu.Lock()
	if len(s.upsertKlineErrors) > 0 {
		err := s.upsertKlineErrors[0]
		s.upsertKlineErrors = s.upsertKlineErrors[1:]
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	atomic.AddInt64(&s.klines, 1)
	return nil
}

func (s *fakeStore) UpsertKlines(_ context.Context, klines []model.Kline) error {
	atomic.AddInt64(&s.klines, int64(len(klines)))
	return nil
}

func (s *fakeStore) SetOpenInterest(context.Context, model.OpenInterest) error {
	return nil
}

func (s *fakeStore) SetLastPrice(ctx context.Context, price model.LastPrice) error {
	if s.lastPriceBlock != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.lastPriceBlock:
		}
	}
	atomic.AddInt64(&s.lastPrices, 1)
	s.mu.Lock()
	if s.lastPriceBySymbol == nil {
		s.lastPriceBySymbol = map[string]model.LastPrice{}
	}
	s.lastPriceBySymbol[price.Symbol] = price
	s.mu.Unlock()
	return nil
}

func (s *fakeStore) SetMarkPrice(context.Context, model.MarkPrice) error {
	return nil
}

func (s *fakeStore) SetBookTicker(context.Context, model.BookTicker) error {
	atomic.AddInt64(&s.bookTickers, 1)
	return nil
}

func (s *fakeStore) AddLiquidation(context.Context, model.Liquidation, int64) error {
	return nil
}

func (s *fakeStore) SetMarketStatus(_ context.Context, status model.MarketStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses = append(s.statuses, status)
	return nil
}

func (s *fakeStore) SetWebSocketStatus(_ context.Context, status model.WebSocketStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wsStatuses = append(s.wsStatuses, status)
	return nil
}

func TestNextStartTimeWithoutExistingData(t *testing.T) {
	c := New(testOptions(), &fakeREST{}, nil, &fakeStore{})

	got, err := c.nextStartTime(context.Background(), "ETHUSDT", "3m")
	if err != nil {
		t.Fatalf("nextStartTime: %v", err)
	}
	if got != 0 {
		t.Fatalf("nextStartTime = %d, want 0", got)
	}
}

func TestNextStartTimeAfterExistingKline(t *testing.T) {
	c := New(testOptions(), &fakeREST{}, nil, &fakeStore{
		lastOpenTime: 1700000000000,
		hasLast:      true,
	})

	got, err := c.nextStartTime(context.Background(), "ETHUSDT", "5m")
	if err != nil {
		t.Fatalf("nextStartTime: %v", err)
	}

	const want int64 = 1700000300000
	if got != want {
		t.Fatalf("nextStartTime = %d, want %d", got, want)
	}
}

func TestBackfillMarksMarketAvailableAfterSuccessfulUpdate(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{}, nil, store)

	if err := c.Backfill(context.Background()); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if len(store.statuses) != 2 {
		t.Fatalf("statuses = %d, want symbol and market statuses", len(store.statuses))
	}
	if !store.statuses[0].Available || store.statuses[0].Symbol != "ETHUSDT" || !store.statuses[1].Available || store.statuses[1].Symbol != "" {
		t.Fatalf("statuses = %#v, want available symbol then market", store.statuses)
	}
}

func TestBackfillStoresOnlyClosedKlines(t *testing.T) {
	currentOpen := time.UnixMilli(1700000120000).Truncate(time.Minute).UnixMilli()
	now := currentOpen + 30000
	rest := &fakeREST{klines: []model.Kline{
		{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: currentOpen - 60000},
		{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: currentOpen},
	}}
	store := &fakeStore{}
	c := New(testOptions(), rest, nil, store)
	c.now = func() time.Time { return time.UnixMilli(now) }

	if err := c.Backfill(context.Background()); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if got := atomic.LoadInt64(&store.klines); got != 1 {
		t.Fatalf("stored klines = %d, want only one closed kline", got)
	}
}

func TestBackfillContinuesAfterPartialFailures(t *testing.T) {
	store := &fakeStore{}
	rest := &fakeREST{
		fetchKlinesErrBySymbol: map[string]error{
			"BADUSDT": errors.New("bad symbol"),
		},
	}
	c := New(Options{
		Symbols:              []string{"BADUSDT", "ETHUSDT"},
		Intervals:            []string{"1m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Second,
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
		StartupLookback:      1,
	}, rest, nil, store)

	if err := c.Backfill(context.Background()); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if rest.fetchKlines != 2 {
		t.Fatalf("FetchKlines calls = %d, want 2", rest.fetchKlines)
	}
	if got := atomic.LoadInt64(&store.klines); got != 1 {
		t.Fatalf("klines = %d, want 1", got)
	}
	if len(store.statuses) != 3 || store.statuses[0].Symbol != "BADUSDT" || store.statuses[0].Available || store.statuses[1].Symbol != "ETHUSDT" || !store.statuses[1].Available || store.statuses[2].Symbol != "" || !store.statuses[2].Available {
		t.Fatalf("statuses = %#v, want unavailable BADUSDT and available ETHUSDT/market", store.statuses)
	}
}

func TestRunMarksMarketUnavailableAfterBackfillFailure(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{fetchKlinesErr: errors.New("exchange unavailable")}, nil, store)

	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected Run to fail")
	}
	if len(store.statuses) < 2 {
		t.Fatalf("statuses = %#v, want symbol and market unavailable", store.statuses)
	}
	if store.statuses[0].Symbol != "ETHUSDT" || store.statuses[0].Available {
		t.Fatalf("symbol status = %#v, want unavailable ETHUSDT", store.statuses[0])
	}
	if store.statuses[0].Reason == "" {
		t.Fatal("expected unavailable reason")
	}
}

func TestPollOpenInterestLimitsSymbols(t *testing.T) {
	symbols := make([]string, 0, maxOpenInterestSymbols+5)
	for index := 0; index < maxOpenInterestSymbols+5; index++ {
		symbols = append(symbols, "SYM"+strconv.Itoa(index)+"USDT")
	}
	rest := &fakeREST{}
	c := New(Options{
		Symbols:              symbols,
		Intervals:            []string{"1m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Second,
		LiquidationLimit:     200,
		PollOpenInterest:     true,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
		StartupLookback:      1,
	}, rest, nil, &fakeStore{})

	c.pollOpenInterest(context.Background())

	if got := len(rest.openInterestSymbols); got != maxOpenInterestSymbols {
		t.Fatalf("open interest calls = %d, want %d", got, maxOpenInterestSymbols)
	}
	if got := rest.openInterestSymbols[len(rest.openInterestSymbols)-1]; got != symbols[maxOpenInterestSymbols-1] {
		t.Fatalf("last open interest symbol = %s, want %s", got, symbols[maxOpenInterestSymbols-1])
	}
}

func TestOpenInterestDoesNotSetMarketAvailable(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{}, nil, store)

	if err := c.HandleOpenInterest(context.Background(), model.OpenInterest{
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		OpenInterest: "100",
	}); err != nil {
		t.Fatalf("HandleOpenInterest: %v", err)
	}
	c.flushLatestEvents(context.Background())

	if len(store.statuses) != 0 {
		t.Fatalf("statuses = %d, want 0", len(store.statuses))
	}
}

func TestBackfillUsesRollingWindowEvenWhenLatestKlineIsCurrent(t *testing.T) {
	rest := &fakeREST{}
	store := &fakeStore{
		lastOpenTime: 1700000000000,
		hasLast:      true,
	}
	c := New(testOptions(), rest, nil, store)
	c.now = func() time.Time {
		return time.UnixMilli(1700000061000)
	}

	if err := c.Backfill(context.Background()); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if rest.fetchKlines != 1 {
		t.Fatalf("FetchKlines calls = %d, want 1", rest.fetchKlines)
	}
	if len(store.statuses) != 2 || store.statuses[0].Symbol != "ETHUSDT" || !store.statuses[0].Available || store.statuses[1].Symbol != "" || !store.statuses[1].Available {
		t.Fatalf("statuses = %#v, want available symbol and market", store.statuses)
	}
}

func TestBackfillThrottlesFetchRequests(t *testing.T) {
	rest := &fakeREST{}
	c := New(Options{
		Symbols:              []string{"ETHUSDT"},
		Intervals:            []string{"1m", "3m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Second,
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
		StartupLookback:      1,
	}, rest, nil, &fakeStore{})

	if err := c.Backfill(context.Background()); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if len(rest.fetchTimes) != 2 {
		t.Fatalf("fetch calls = %d, want 2", len(rest.fetchTimes))
	}
	if elapsed := rest.fetchTimes[1].Sub(rest.fetchTimes[0]); elapsed < backfillRequestInterval {
		t.Fatalf("fetch interval = %s, want >= %s", elapsed, backfillRequestInterval)
	}
}

func TestBackfillUsesIntervalPriority(t *testing.T) {
	rest := &fakeREST{}
	c := New(Options{
		Symbols:              []string{"ETHUSDT", "BTCUSDT"},
		Intervals:            []string{"1h", "1m", "5m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Second,
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
		StartupLookback:      1,
	}, rest, nil, &fakeStore{})

	if err := c.Backfill(context.Background()); err != nil {
		t.Fatalf("Backfill: %v", err)
	}

	got := make([]string, 0, len(rest.fetchRequests))
	for _, request := range rest.fetchRequests {
		got = append(got, request.symbol+":"+request.interval)
	}
	want := []string{
		"ETHUSDT:1m",
		"ETHUSDT:5m",
		"ETHUSDT:1h",
		"BTCUSDT:1m",
		"BTCUSDT:5m",
		"BTCUSDT:1h",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fetch order = %#v, want %#v", got, want)
	}
}

func TestBackfillRequestsCurrentRollingWindow(t *testing.T) {
	now := time.UnixMilli(1700000120000).Truncate(time.Minute).Add(30 * time.Second)
	rest := &fakeREST{klines: []model.Kline{}}
	options := testOptions()
	options.StartupLookback = 310
	c := New(options, rest, nil, &fakeStore{})
	c.now = func() time.Time { return now }

	if err := c.Backfill(context.Background()); err == nil {
		t.Fatal("expected incomplete rolling window")
	}
	request := rest.fetchRequests[0]
	wantEnd := now.Truncate(time.Minute).Add(-time.Minute).UnixMilli()
	wantStart := wantEnd - 309*time.Minute.Milliseconds()
	if request.start != wantStart {
		t.Fatalf("start = %d, want %d", request.start, wantStart)
	}
	if request.limit != 310 {
		t.Fatalf("limit = %d, want 310", request.limit)
	}
}

func TestBackfillReusesCompleteRecentWindow(t *testing.T) {
	now := time.UnixMilli(1700000120000).Truncate(time.Minute).Add(30 * time.Second)
	end := now.Truncate(time.Minute).Add(-time.Minute).UnixMilli()
	klines := make([]model.Kline, 0, 3)
	for index := int64(2); index >= 0; index-- {
		klines = append(klines, model.Kline{OpenTime: end - index*time.Minute.Milliseconds(), IsClosed: true})
	}
	store := &fakeStore{rangeKlines: klines}
	rest := &fakeREST{}
	options := testOptions()
	options.StartupLookback = 3
	c := New(options, rest, nil, store)
	c.now = func() time.Time { return now }

	if err := c.Backfill(context.Background()); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if rest.fetchKlines != 0 {
		t.Fatalf("FetchKlines calls = %d, want 0", rest.fetchKlines)
	}
}

func TestAggregateRecentKlinesBuildsExactTargetWindow(t *testing.T) {
	const minute = int64(time.Minute / time.Millisecond)
	rule := aggregator.Rule{
		Exchange:       "gate",
		Market:         "usdt",
		SourceInterval: "1m",
		TargetInterval: "3m",
	}
	source := make([]model.Kline, 0, 6)
	for index := int64(0); index < 6; index++ {
		source = append(source, model.Kline{
			Exchange: "gate", Market: "usdt", Symbol: "ETH_USDT", Interval: "1m",
			OpenTime: index * minute, IsClosed: true, Open: "1", High: "2", Low: "1", Close: "2",
		})
	}

	got, err := aggregateRecentKlines(rule, "ETH_USDT", source, 2, 6*minute)
	if err != nil {
		t.Fatalf("aggregateRecentKlines: %v", err)
	}
	if len(got) != 2 || got[0].OpenTime != 0 || got[1].OpenTime != 3*minute {
		t.Fatalf("aggregated window = %#v", got)
	}
}

func TestOpenKlineIsCoalescedLatestEvent(t *testing.T) {
	event := collectorEvent{
		eventType: collectorEventKline,
		kline: model.Kline{
			Symbol:   "ETHUSDT",
			Interval: "1m",
			IsClosed: false,
		},
	}

	if !event.isLatest() {
		t.Fatal("open kline should be treated as latest event")
	}
	if event.isCritical() {
		t.Fatal("open kline should not be treated as critical event")
	}
	if got, want := event.latestKey(), "kline:ETHUSDT:1m"; got != want {
		t.Fatalf("latest key = %q, want %q", got, want)
	}
}

func TestClosedKlineIsCriticalEvent(t *testing.T) {
	event := collectorEvent{
		eventType: collectorEventKline,
		kline: model.Kline{
			Symbol:   "ETHUSDT",
			Interval: "1m",
			IsClosed: true,
		},
	}

	if event.isLatest() {
		t.Fatal("closed kline should not be treated as latest event")
	}
	if !event.isCritical() {
		t.Fatal("closed kline should be treated as critical event")
	}
}

func TestBackfillThrottleWaitCanBeCanceled(t *testing.T) {
	rest := &fakeREST{}
	c := New(Options{
		Symbols:              []string{"ETHUSDT"},
		Intervals:            []string{"1m", "3m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Second,
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
	}, rest, nil, &fakeStore{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.lastBackfillRequest = time.Now()

	if err := c.waitBackfillRequest(ctx); err == nil {
		t.Fatal("expected canceled context error")
	}
}

func TestNextReconnectDelay(t *testing.T) {
	tests := []struct {
		name                string
		base                time.Duration
		consecutiveFailures int64
		want                time.Duration
	}{
		{
			name:                "first failure uses base delay",
			base:                time.Second,
			consecutiveFailures: 1,
			want:                time.Second,
		},
		{
			name:                "consecutive failures double delay",
			base:                time.Second,
			consecutiveFailures: 4,
			want:                8 * time.Second,
		},
		{
			name:                "zero base falls back to one second",
			base:                0,
			consecutiveFailures: 2,
			want:                2 * time.Second,
		},
		{
			name:                "delay is capped",
			base:                30 * time.Second,
			consecutiveFailures: 3,
			want:                time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextReconnectDelay(tt.base, tt.consecutiveFailures)
			if got != tt.want {
				t.Fatalf("nextReconnectDelay = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestShardStreams(t *testing.T) {
	streams := make([]exchange.Stream, 0, 11)
	for i := 0; i < 11; i++ {
		streams = append(streams, exchange.Stream{Symbol: "ETHUSDT"})
	}

	shards := distributeStreams(streams, 3)

	if len(shards) != 3 {
		t.Fatalf("shards = %d, want 3", len(shards))
	}
	if len(shards[0]) != 4 || len(shards[1]) != 4 || len(shards[2]) != 3 {
		t.Fatalf("shard sizes = %d,%d,%d; want 4,4,3", len(shards[0]), len(shards[1]), len(shards[2]))
	}
}

func TestWebSocketConnections(t *testing.T) {
	tests := []struct {
		name        string
		options     Options
		streamCount int
		want        int
	}{
		{
			name:        "no streams",
			streamCount: 0,
			want:        0,
		},
		{
			name:        "default uses stream density",
			streamCount: 201,
			want:        3,
		},
		{
			name:        "configured connection count",
			options:     Options{WebSocketConnections: 8},
			streamCount: 201,
			want:        8,
		},
		{
			name:        "configured count is capped by streams",
			options:     Options{WebSocketConnections: 8},
			streamCount: 3,
			want:        3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := webSocketConnections(tt.options, tt.streamCount)
			if got != tt.want {
				t.Fatalf("webSocketConnections = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRunWebSocketLoopStartsShardsWithoutBackfill(t *testing.T) {
	rest := &fakeREST{}
	store := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	ws := &fakeWS{
		runs:      make(chan []exchange.Stream, 1),
		remaining: 1,
		cancel:    cancel,
	}
	c := New(Options{
		Symbols:              []string{"ETHUSDT", "BTCUSDT", "SOLUSDT"},
		Intervals:            []string{"1m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Millisecond,
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
	}, rest, ws, store)

	if err := c.runWebSocketLoop(ctx); err != nil {
		t.Fatalf("runWebSocketLoop: %v", err)
	}

	if rest.fetchKlines != 0 {
		t.Fatalf("FetchKlines calls = %d, want 0", rest.fetchKlines)
	}
	if len(ws.runs) != 1 {
		t.Fatalf("websocket runs = %d, want 1", len(ws.runs))
	}
	if got := len(<-ws.runs); got != 15 {
		t.Fatalf("shard streams = %d, want 15", got)
	}
}

func TestRunWebSocketLoopDistributesStreamsAcrossConfiguredConnections(t *testing.T) {
	rest := &fakeREST{}
	store := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	ws := &fakeWS{
		runs:      make(chan []exchange.Stream, 3),
		remaining: 3,
		cancel:    cancel,
	}
	c := New(Options{
		Symbols:              []string{"ETHUSDT", "BTCUSDT", "SOLUSDT"},
		Intervals:            []string{"1m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Millisecond,
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
		WebSocketConnections: 3,
	}, rest, ws, store)

	if err := c.runWebSocketLoop(ctx); err != nil {
		t.Fatalf("runWebSocketLoop: %v", err)
	}

	total := 0
	sizes := make([]int, 0, 3)
	for len(ws.runs) > 0 {
		size := len(<-ws.runs)
		total += size
		sizes = append(sizes, size)
	}
	if total != 15 {
		t.Fatalf("total shard streams = %d, want 15", total)
	}
	if !reflect.DeepEqual(sizes, []int{5, 5, 5}) {
		t.Fatalf("shard sizes = %v, want [5 5 5]", sizes)
	}
}

func TestAdaptiveEventSettings(t *testing.T) {
	small := Options{
		Symbols:   []string{"ETHUSDT"},
		Intervals: []string{"1m"},
	}
	if got := adaptiveEventQueueSize(small); got != minEventQueueSize {
		t.Fatalf("small adaptiveEventQueueSize = %d, want %d", got, minEventQueueSize)
	}
	if got := adaptiveEventWorkers(small); got != defaultEventWorkers {
		t.Fatalf("small adaptiveEventWorkers = %d, want %d", got, defaultEventWorkers)
	}

	large := Options{
		Symbols:   make([]string, 1000),
		Intervals: []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h"},
	}
	if got := adaptiveEventQueueSize(large); got != maxEventQueueSize {
		t.Fatalf("large adaptiveEventQueueSize = %d, want %d", got, maxEventQueueSize)
	}
	if got := adaptiveEventWorkers(large); got != maxEventWorkers {
		t.Fatalf("large adaptiveEventWorkers = %d, want %d", got, maxEventWorkers)
	}
}

func TestEventWorkerProcessesQueuedCriticalEvent(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{}, nil, store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	c.startEventWorkers(ctx, &wg)
	defer func() {
		cancel()
		wg.Wait()
	}()

	err := c.HandleKline(ctx, model.Kline{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		IsClosed: true,
	})
	if err != nil {
		t.Fatalf("HandleKline: %v", err)
	}

	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if atomic.LoadInt64(&store.klines) == 1 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("expected worker to process kline")
		case <-ticker.C:
		}
	}
}

func TestStatsTracksProcessedAndCoalescedEvents(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{}, nil, store)

	if err := c.HandleLastPrice(context.Background(), model.LastPrice{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Price:    "100",
	}); err != nil {
		t.Fatalf("HandleLastPrice: %v", err)
	}
	if err := c.HandleLastPrice(context.Background(), model.LastPrice{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Price:    "101",
	}); err != nil {
		t.Fatalf("HandleLastPrice: %v", err)
	}
	if err := c.HandleLastPrice(context.Background(), model.LastPrice{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "BTCUSDT",
		Price:    "200",
	}); err != nil {
		t.Fatalf("HandleLastPrice: %v", err)
	}

	c.flushLatestEvents(context.Background())

	stats := c.Stats()
	if stats.CoalescedLatestEvents != 1 {
		t.Fatalf("CoalescedLatestEvents = %d, want 1", stats.CoalescedLatestEvents)
	}
	if stats.FlushedLatestEvents != 2 {
		t.Fatalf("FlushedLatestEvents = %d, want 2", stats.FlushedLatestEvents)
	}
	if stats.ProcessedEvents != 2 {
		t.Fatalf("ProcessedEvents = %d, want 2", stats.ProcessedEvents)
	}
	if got := atomic.LoadInt64(&store.lastPrices); got != 2 {
		t.Fatalf("lastPrices = %d, want 2", got)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if got := store.lastPriceBySymbol["ETHUSDT"].Price; got != "101" {
		t.Fatalf("ETHUSDT price = %q, want 101", got)
	}
}

func TestStatsTracksEventLatencyAndOutOfOrderEvents(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{}, nil, store)
	now := time.UnixMilli(10_000)
	c.now = func() time.Time { return now }

	for _, eventTime := range []int64{9_900, 9_800} {
		if err := c.HandleKline(context.Background(), model.Kline{
			Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m",
			EventTime: eventTime, IsClosed: true,
		}); err != nil {
			t.Fatalf("HandleKline: %v", err)
		}
	}
	now = time.UnixMilli(10_025)
	if err := c.processEvent(context.Background(), <-c.events.queue); err != nil {
		t.Fatalf("processEvent: %v", err)
	}

	stats := c.Stats()
	if stats.LastEventReceivedAt != 10_000 || stats.LastEventProcessedAt != 10_025 {
		t.Fatalf("event times = received %d processed %d", stats.LastEventReceivedAt, stats.LastEventProcessedAt)
	}
	if stats.SourceDelayMaxMillis != 200 {
		t.Fatalf("SourceDelayMaxMillis = %d, want 200", stats.SourceDelayMaxMillis)
	}
	if stats.QueueDelayMaxMillis != 25 {
		t.Fatalf("QueueDelayMaxMillis = %d, want 25", stats.QueueDelayMaxMillis)
	}
	if stats.OutOfOrderEvents != 1 {
		t.Fatalf("OutOfOrderEvents = %d, want 1", stats.OutOfOrderEvents)
	}
}

func TestCollectorEnforcesKlineVersionTransitions(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{}, nil, store)
	base := model.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: 1_000}
	cases := []struct {
		eventTime int64
		closed    bool
	}{
		{eventTime: 100, closed: false},
		{eventTime: 110, closed: true},
		{eventTime: 120, closed: false},
		{eventTime: 105, closed: true},
		{eventTime: 110, closed: true},
		{eventTime: 120, closed: true},
	}
	for _, item := range cases {
		kline := base
		kline.EventTime = item.eventTime
		kline.IsClosed = item.closed
		if err := c.processEvent(context.Background(), collectorEvent{eventType: collectorEventKline, kline: kline}); err != nil {
			t.Fatalf("processEvent(%d, %t): %v", item.eventTime, item.closed, err)
		}
	}
	if got := atomic.LoadInt64(&store.klines); got != 3 {
		t.Fatalf("stored klines = %d, want open, closed and corrected closed", got)
	}
	stats := c.Stats()
	if stats.OpenAfterClosedEvents != 1 || stats.StaleKlineEvents != 1 || stats.DuplicateKlineEvents != 1 {
		t.Fatalf("kline decisions = open_after_closed %d stale %d duplicate %d", stats.OpenAfterClosedEvents, stats.StaleKlineEvents, stats.DuplicateKlineEvents)
	}
}

func TestCollectorAllowsReplayAfterConcurrentKlineWriteFailures(t *testing.T) {
	store := &fakeStore{upsertKlineErrors: []error{errors.New("first write failed"), errors.New("second write failed")}}
	c := New(testOptions(), &fakeREST{}, nil, store)
	base := model.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: 1_000, IsClosed: true}

	var wg sync.WaitGroup
	for _, eventTime := range []int64{100, 110} {
		wg.Add(1)
		go func(eventTime int64) {
			defer wg.Done()
			kline := base
			kline.EventTime = eventTime
			_ = c.processEvent(context.Background(), collectorEvent{eventType: collectorEventKline, kline: kline})
		}(eventTime)
	}
	wg.Wait()

	replay := base
	replay.EventTime = 110
	if err := c.processEvent(context.Background(), collectorEvent{eventType: collectorEventKline, kline: replay}); err != nil {
		t.Fatalf("replay after failed writes: %v", err)
	}
	if got := atomic.LoadInt64(&store.klines); got != 1 {
		t.Fatalf("successful stored klines = %d, want replay to persist once", got)
	}
}

func TestCollectorRetainsBoundedKlineVersions(t *testing.T) {
	c := New(testOptions(), &fakeREST{}, nil, &fakeStore{})
	for index := int64(0); index < klineVersionRetention+3; index++ {
		kline := model.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: index, EventTime: index + 1, IsClosed: true}
		if err := c.processEvent(context.Background(), collectorEvent{eventType: collectorEventKline, kline: kline}); err != nil {
			t.Fatal(err)
		}
	}
	c.klines.versionMu.Lock()
	got := len(c.klines.versions["binance:um:ETHUSDT:1m"])
	c.klines.versionMu.Unlock()
	if got != klineVersionRetention {
		t.Fatalf("retained versions = %d, want %d", got, klineVersionRetention)
	}
}

func TestStartupRESTVersionProtectsClosedKlineFromWebSocketOpen(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{}, nil, store)
	restKline := model.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: 1_000, IsClosed: true}
	c.rememberStoredKlines([]model.Kline{restKline}, klineSourceStartupREST, true)

	open := restKline
	open.IsClosed = false
	open.EventTime = 200
	if err := c.processEvent(context.Background(), collectorEvent{eventType: collectorEventKline, kline: open, klineSource: klineSourceWebSocket}); err != nil {
		t.Fatal(err)
	}
	corrected := restKline
	corrected.EventTime = 300
	if err := c.processEvent(context.Background(), collectorEvent{eventType: collectorEventKline, kline: corrected, klineSource: klineSourceWebSocket}); err != nil {
		t.Fatal(err)
	}

	if got := atomic.LoadInt64(&store.klines); got != 1 {
		t.Fatalf("websocket store writes = %d, want corrected closed only", got)
	}
	stats := c.Stats()
	if stats.StartupRESTKlines != 1 || stats.WebSocketKlineEvents != 1 || stats.OpenAfterClosedEvents != 1 || stats.KlineCorrections != 1 {
		t.Fatalf("source stats = %#v", stats)
	}
}

func TestCollectorDetectsClosedKlineGapFromStartupCursor(t *testing.T) {
	const minute = int64(time.Minute / time.Millisecond)
	options := testOptions()
	publisher := &fakeGapPublisher{}
	options.GapPublisher = publisher
	c := New(options, &fakeREST{}, nil, &fakeStore{})
	startup := []model.Kline{
		{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: minute, IsClosed: true},
		{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: 2 * minute, IsClosed: true},
	}
	c.rememberStoredKlines(startup, klineSourceStartupREST, false)
	next := model.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: 5 * minute, EventTime: 6 * minute, IsClosed: true}
	if err := c.processEvent(context.Background(), collectorEvent{eventType: collectorEventKline, kline: next, klineSource: klineSourceWebSocket}); err != nil {
		t.Fatal(err)
	}
	stats := c.Stats()
	if stats.KlineGapsDetected != 1 || stats.KlineGapBars != 2 || stats.KlineGapRequests != 1 {
		t.Fatalf("gap stats = detected %d bars %d, want 1 and 2", stats.KlineGapsDetected, stats.KlineGapBars)
	}
	if len(publisher.tasks) != 1 {
		t.Fatalf("published tasks = %d, want 1", len(publisher.tasks))
	}
	task := publisher.tasks[0]
	if task.Source != "collector_gap" || task.Reason != "closed_kline_gap" || task.Start != "197001010003" || task.End != "197001010005" || task.Timezone != "UTC" || !reflect.DeepEqual(task.Intervals, []string{"1m"}) {
		t.Fatalf("gap task = %#v", task)
	}

	correction := next
	correction.OpenTime = 4 * minute
	correction.EventTime = 7 * minute
	if err := c.processEvent(context.Background(), collectorEvent{eventType: collectorEventKline, kline: correction, klineSource: klineSourceWebSocket}); err != nil {
		t.Fatal(err)
	}
	stats = c.Stats()
	if stats.KlineGapsDetected != 1 || stats.KlineGapBars != 2 {
		t.Fatalf("late correction changed gap stats: %#v", stats)
	}
}

func TestStartupCursorAndRealtimeContinuityAreConcurrentSafe(t *testing.T) {
	const minute = int64(time.Minute / time.Millisecond)
	c := New(testOptions(), &fakeREST{}, nil, &fakeStore{})
	base := model.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", IsClosed: true}

	var wg sync.WaitGroup
	for index := int64(1); index <= 100; index++ {
		kline := base
		kline.OpenTime = index * minute
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.rememberStoredKlines([]model.Kline{kline}, klineSourceStartupREST, false)
		}()
		go func() {
			defer wg.Done()
			c.recordKlineContinuity(context.Background(), kline)
		}()
	}
	wg.Wait()

	streamKey := klineStreamKey(base)
	c.klines.continuityMu.Lock()
	got := c.klines.lastClosedOpenTimes[streamKey]
	c.klines.continuityMu.Unlock()
	if got != 100*minute {
		t.Fatalf("last closed open time = %d, want %d", got, 100*minute)
	}
}

func TestCollectorGapPublishFailureDoesNotFailKlineStore(t *testing.T) {
	const minute = int64(time.Minute / time.Millisecond)
	options := testOptions()
	publisher := &fakeGapPublisher{err: errors.New("nats unavailable")}
	options.GapPublisher = publisher
	store := &fakeStore{}
	c := New(options, &fakeREST{}, nil, store)
	c.rememberStoredKlines([]model.Kline{{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: minute, IsClosed: true}}, klineSourceStartupREST, false)
	next := model.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", OpenTime: 3 * minute, EventTime: 4 * minute, IsClosed: true}
	if err := c.processEvent(context.Background(), collectorEvent{eventType: collectorEventKline, kline: next, klineSource: klineSourceWebSocket}); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if got := atomic.LoadInt64(&store.klines); got != 1 {
		t.Fatalf("stored klines = %d, want 1", got)
	}
	if stats := c.Stats(); stats.KlineGapRequestErrors != 1 || stats.KlineGapRequests != 0 {
		t.Fatalf("gap request stats = %#v", stats)
	}
	publisher.err = nil
	following := next
	following.OpenTime = 4 * minute
	following.EventTime = 5 * minute
	if err := c.processEvent(context.Background(), collectorEvent{eventType: collectorEventKline, kline: following, klineSource: klineSourceWebSocket}); err != nil {
		t.Fatalf("process following event: %v", err)
	}
	if len(publisher.tasks) != 2 {
		t.Fatalf("published attempts = %d, want initial failure and retry", len(publisher.tasks))
	}
	if publisher.tasks[0].Start != publisher.tasks[1].Start || publisher.tasks[0].End != publisher.tasks[1].End {
		t.Fatalf("retry changed gap range: first %#v retry %#v", publisher.tasks[0], publisher.tasks[1])
	}
	if stats := c.Stats(); stats.KlineGapRequestErrors != 1 || stats.KlineGapRequests != 1 {
		t.Fatalf("gap retry stats = %#v", stats)
	}
}

func TestLatestEventBypassesFullCriticalQueue(t *testing.T) {
	c := New(Options{
		Symbols:              []string{"ETHUSDT"},
		Intervals:            []string{"1m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Second,
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
	}, &fakeREST{}, nil, &fakeStore{})
	c.events.queue = make(chan collectorEvent, 1)
	c.events.queue <- collectorEvent{eventType: collectorEventKline}

	startedAt := time.Now()
	err := c.HandleLastPrice(context.Background(), model.LastPrice{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
	})
	if err != nil {
		t.Fatalf("HandleLastPrice: %v", err)
	}
	if time.Since(startedAt) > 500*time.Millisecond {
		t.Fatal("latest event should bypass full critical queue")
	}
	if len(c.events.queue) != 1 {
		t.Fatalf("queue length = %d, want 1", len(c.events.queue))
	}
	if got := len(c.events.latest); got != 1 {
		t.Fatalf("latest events = %d, want 1", got)
	}
}

func TestCriticalEventWaitsWhenQueueIsFull(t *testing.T) {
	c := New(Options{
		Symbols:              []string{"ETHUSDT"},
		Intervals:            []string{"1m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Second,
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
	}, &fakeREST{}, nil, &fakeStore{})
	c.events.queue = make(chan collectorEvent, 1)
	c.events.queue <- collectorEvent{eventType: collectorEventLastPrice}

	done := make(chan error, 1)
	go func() {
		done <- c.HandleKline(context.Background(), model.Kline{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			IsClosed: true,
		})
	}()

	select {
	case err := <-done:
		t.Fatalf("critical event returned before queue space was available: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	<-c.events.queue
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("HandleKline: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("critical event did not enqueue after queue space was available")
	}
}

func TestWaitForEventDrainCompletesWithinTimeout(t *testing.T) {
	c := New(testOptions(), &fakeREST{}, nil, &fakeStore{})
	c.addPendingEvent()
	go func() {
		time.Sleep(20 * time.Millisecond)
		c.completePendingEvent()
	}()

	if !c.waitForEventDrain(time.Second) {
		t.Fatal("waitForEventDrain timed out")
	}
	if pending := c.events.pending.Load(); pending != 0 {
		t.Fatalf("pending = %d, want 0", pending)
	}
}

func TestWaitForEventDrainTimesOutAndCanBeReused(t *testing.T) {
	c := New(testOptions(), &fakeREST{}, nil, &fakeStore{})
	c.addPendingEvent()
	startedAt := time.Now()
	if c.waitForEventDrain(20 * time.Millisecond) {
		t.Fatal("waitForEventDrain unexpectedly completed")
	}
	if elapsed := time.Since(startedAt); elapsed < 15*time.Millisecond || elapsed > time.Second {
		t.Fatalf("elapsed = %s, want bounded timeout", elapsed)
	}

	c.completePendingEvent()
	if !c.waitForEventDrain(20 * time.Millisecond) {
		t.Fatal("waitForEventDrain did not recover after pending event completed")
	}
}

func TestLatestEventFinalFlushUsesDrainTimeout(t *testing.T) {
	blocked := make(chan struct{})
	store := &fakeStore{lastPriceBlock: blocked}
	options := testOptions()
	options.EventDrainTimeout = 20 * time.Millisecond
	c := New(options, &fakeREST{}, nil, store)
	if err := c.HandleLastPrice(context.Background(), model.LastPrice{Exchange: "binance", Market: "um", Symbol: "ETHUSDT"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startedAt := time.Now()
	c.runLatestEventFlusher(ctx)
	if elapsed := time.Since(startedAt); elapsed < 15*time.Millisecond || elapsed > time.Second {
		t.Fatalf("final flush elapsed = %s, want bounded timeout", elapsed)
	}
	if stats := c.Stats(); stats.ProcessEventErrors != 1 {
		t.Fatalf("process event errors = %d, want 1", stats.ProcessEventErrors)
	}
}

func testOptions() Options {
	return Options{
		Symbols:              []string{"ETHUSDT"},
		Intervals:            []string{"1m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Second,
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
		StartupLookback:      1,
	}
}

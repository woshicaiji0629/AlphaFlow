package collector

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	bookTickers       int64
}

type fakeREST struct {
	fetchKlinesErr         error
	fetchKlinesErrBySymbol map[string]error
	fetchKlines            int
	fetchTimes             []time.Time
	openInterestSymbols    []string
	openInterestErr        error
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
	_ string,
	_ int,
	_ int64,
) ([]model.Kline, error) {
	r.fetchKlines++
	r.fetchTimes = append(r.fetchTimes, time.Now())
	if err := r.fetchKlinesErrBySymbol[symbol]; err != nil {
		return nil, err
	}
	if r.fetchKlinesErr != nil {
		return nil, r.fetchKlinesErr
	}
	return []model.Kline{{
		Exchange:  "binance",
		Market:    "um",
		Symbol:    "ETHUSDT",
		Interval:  "1m",
		OpenTime:  1700000000000,
		CloseTime: 1700000059999,
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

func (s *fakeStore) UpsertKline(context.Context, model.Kline) error {
	atomic.AddInt64(&s.klines, 1)
	return nil
}

func (s *fakeStore) SetOpenInterest(context.Context, model.OpenInterest) error {
	return nil
}

func (s *fakeStore) SetLastPrice(_ context.Context, price model.LastPrice) error {
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
	s.statuses = append(s.statuses, status)
	return nil
}

func (s *fakeStore) SetWebSocketStatus(_ context.Context, status model.WebSocketStatus) error {
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
	if len(store.statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(store.statuses))
	}
	if !store.statuses[0].Available {
		t.Fatalf("market status available = false, want true")
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
	if len(store.statuses) != 1 || !store.statuses[0].Available {
		t.Fatalf("statuses = %#v, want one available status", store.statuses)
	}
}

func TestRunMarksMarketUnavailableAfterBackfillFailure(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{fetchKlinesErr: errors.New("exchange unavailable")}, nil, store)

	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected Run to fail")
	}
	if len(store.statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(store.statuses))
	}
	if store.statuses[0].Available {
		t.Fatalf("market status available = true, want false")
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

func TestBackfillSkipsWhenNextStartTimeHasNoClosedWindow(t *testing.T) {
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
	if rest.fetchKlines != 0 {
		t.Fatalf("FetchKlines calls = %d, want 0", rest.fetchKlines)
	}
	if len(store.statuses) != 0 {
		t.Fatalf("statuses = %d, want 0", len(store.statuses))
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

	shards := shardStreams(streams, 5)

	if len(shards) != 3 {
		t.Fatalf("shards = %d, want 3", len(shards))
	}
	if len(shards[0]) != 5 || len(shards[1]) != 5 || len(shards[2]) != 1 {
		t.Fatalf("shard sizes = %d,%d,%d; want 5,5,1", len(shards[0]), len(shards[1]), len(shards[2]))
	}
}

func TestRunWebSocketLoopBackfillsOnceForShards(t *testing.T) {
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

	if rest.fetchKlines != 3 {
		t.Fatalf("FetchKlines calls = %d, want 3", rest.fetchKlines)
	}
	if len(ws.runs) != 1 {
		t.Fatalf("websocket runs = %d, want 1", len(ws.runs))
	}
	if got := len(<-ws.runs); got != 15 {
		t.Fatalf("shard streams = %d, want 15", got)
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
	c.eventQueue = make(chan collectorEvent, 1)
	c.eventQueue <- collectorEvent{eventType: collectorEventKline}

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
	if len(c.eventQueue) != 1 {
		t.Fatalf("queue length = %d, want 1", len(c.eventQueue))
	}
	if got := len(c.latestEvents); got != 1 {
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
	c.eventQueue = make(chan collectorEvent, 1)
	c.eventQueue <- collectorEvent{eventType: collectorEventLastPrice}

	done := make(chan error, 1)
	go func() {
		done <- c.HandleKline(context.Background(), model.Kline{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
		})
	}()

	select {
	case err := <-done:
		t.Fatalf("critical event returned before queue space was available: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	<-c.eventQueue
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("HandleKline: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("critical event did not enqueue after queue space was available")
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
	}
}

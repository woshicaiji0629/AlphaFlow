package store

import (
	"context"
	"errors"
	"testing"

	"alphaflow/go-service/market-data/internal/model"
)

type fakeClickHouseWriter struct {
	klines         []model.Kline
	writeKlinesErr error
}

func (w *fakeClickHouseWriter) WriteKline(ctx context.Context, kline model.Kline) error {
	return w.WriteKlines(ctx, []model.Kline{kline})
}

func (w *fakeClickHouseWriter) WriteKlines(_ context.Context, klines []model.Kline) error {
	if w.writeKlinesErr != nil {
		return w.writeKlinesErr
	}
	w.klines = append(w.klines, klines...)
	return nil
}

func (w *fakeClickHouseWriter) Close() error {
	return nil
}

func TestMarketStoreCoalescesLatestWrites(t *testing.T) {
	s := NewMarketStore(&RedisStore{}, nil, MarketStoreOptions{})

	if err := s.SetLastPrice(context.Background(), model.LastPrice{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Price:    "100",
	}); err != nil {
		t.Fatalf("SetLastPrice: %v", err)
	}
	if err := s.SetLastPrice(context.Background(), model.LastPrice{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Price:    "101",
	}); err != nil {
		t.Fatalf("SetLastPrice: %v", err)
	}
	if err := s.SetMarkPrice(context.Background(), model.MarkPrice{
		Exchange:  "binance",
		Market:    "um",
		Symbol:    "ETHUSDT",
		MarkPrice: "99",
	}); err != nil {
		t.Fatalf("SetMarkPrice: %v", err)
	}
	if err := s.SetBookTicker(context.Background(), model.BookTicker{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		BidPrice: "100",
		AskPrice: "102",
	}); err != nil {
		t.Fatalf("SetBookTicker: %v", err)
	}
	if err := s.SetOpenInterest(context.Background(), model.OpenInterest{
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		OpenInterest: "100",
	}); err != nil {
		t.Fatalf("SetOpenInterest: %v", err)
	}
	if err := s.SetOpenInterest(context.Background(), model.OpenInterest{
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		OpenInterest: "101",
	}); err != nil {
		t.Fatalf("SetOpenInterest: %v", err)
	}
	if err := s.SetLatestIndicator(context.Background(), model.IndicatorSnapshot{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1000,
		Values:   map[string]string{"rsi": "50"},
	}); err != nil {
		t.Fatalf("SetLatestIndicator: %v", err)
	}
	if err := s.SetLatestIndicator(context.Background(), model.IndicatorSnapshot{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1000,
		Values:   map[string]string{"rsi": "51"},
	}); err != nil {
		t.Fatalf("SetLatestIndicator: %v", err)
	}
	if err := s.SetIndicatorRealtime(context.Background(), model.IndicatorRealtimeSnapshot{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1000,
		Kline: model.Kline{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 1000,
			Close:    "100",
		},
		Values: map[string]string{"rsi": "50"},
	}); err != nil {
		t.Fatalf("SetIndicatorRealtime: %v", err)
	}
	if err := s.SetIndicatorRealtime(context.Background(), model.IndicatorRealtimeSnapshot{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1000,
		Kline: model.Kline{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 1000,
			Close:    "101",
		},
		Values: map[string]string{"rsi": "51"},
	}); err != nil {
		t.Fatalf("SetIndicatorRealtime: %v", err)
	}
	s.latestMu.Lock()
	s.openKlines[klineLatestKey(model.Kline{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
	})] = model.Kline{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1000,
		Close:    "100",
		IsClosed: false,
	}
	s.openKlines[klineLatestKey(model.Kline{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
	})] = model.Kline{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1000,
		Close:    "101",
		IsClosed: false,
	}
	s.latestMu.Unlock()

	batch := s.drainLatest()
	if len(batch.lastPrices) != 1 {
		t.Fatalf("last prices = %d, want 1", len(batch.lastPrices))
	}
	if batch.lastPrices[0].Price != "101" {
		t.Fatalf("last price = %q, want 101", batch.lastPrices[0].Price)
	}
	if len(batch.markPrices) != 1 {
		t.Fatalf("mark prices = %d, want 1", len(batch.markPrices))
	}
	if len(batch.bookTickers) != 1 {
		t.Fatalf("book tickers = %d, want 1", len(batch.bookTickers))
	}
	if len(batch.openInterests) != 1 {
		t.Fatalf("open interests = %d, want 1", len(batch.openInterests))
	}
	if batch.openInterests[0].OpenInterest != "101" {
		t.Fatalf("open interest = %q, want 101", batch.openInterests[0].OpenInterest)
	}
	if len(batch.openKlines) != 1 {
		t.Fatalf("open klines = %d, want 1", len(batch.openKlines))
	}
	if batch.openKlines[0].Close != "101" {
		t.Fatalf("open kline close = %q, want 101", batch.openKlines[0].Close)
	}
	if len(batch.indicators) != 1 {
		t.Fatalf("indicators = %d, want 1", len(batch.indicators))
	}
	if batch.indicators[0].Values["rsi"] != "51" {
		t.Fatalf("indicator rsi = %q, want 51", batch.indicators[0].Values["rsi"])
	}
	if len(batch.indicatorRTs) != 1 {
		t.Fatalf("indicator realtime snapshots = %d, want 1", len(batch.indicatorRTs))
	}
	if batch.indicatorRTs[0].Kline.Close != "101" {
		t.Fatalf("indicator realtime close = %q, want 101", batch.indicatorRTs[0].Kline.Close)
	}
	if batch := s.drainLatest(); len(batch.lastPrices) != 0 ||
		len(batch.markPrices) != 0 ||
		len(batch.bookTickers) != 0 ||
		len(batch.openInterests) != 0 ||
		len(batch.openKlines) != 0 ||
		len(batch.indicators) != 0 ||
		len(batch.indicatorWins) != 0 ||
		len(batch.indicatorRTs) != 0 {
		t.Fatalf("drainLatest after drain = %#v, want empty", batch)
	}
}

func TestMarketStoreBuffersClickHouseWrites(t *testing.T) {
	s := NewMarketStore(&RedisStore{}, nil, MarketStoreOptions{})
	s.clickhouse = &fakeClickHouseWriter{}

	s.enqueueClickHouseKline(model.Kline{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1000,
		IsClosed: true,
	})

	batch := s.drainClickHouse(10)
	if len(batch.klines) != 1 {
		t.Fatalf("klines = %d, want 1", len(batch.klines))
	}
	if batch := s.drainClickHouse(10); len(batch.klines) != 0 {
		t.Fatalf("drainClickHouse after drain = %#v, want empty", batch)
	}
}

func TestLatestClosedKlinesAfterSkipsStaleKlines(t *testing.T) {
	klines := []model.Kline{
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 3000,
			IsClosed: true,
		},
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 2000,
			IsClosed: true,
		},
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "BTCUSDT",
			Interval: "1m",
			OpenTime: 1000,
			IsClosed: true,
		},
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "BTCUSDT",
			Interval: "1m",
			OpenTime: 2000,
			IsClosed: true,
		},
	}
	lastOpenTimes := map[string]int64{
		model.RedisKey("binance", "um", "ETHUSDT", "1m"): 3000,
		model.RedisKey("binance", "um", "BTCUSDT", "1m"): 500,
	}

	latest := latestClosedKlinesAfter(klines, lastOpenTimes)

	if len(latest) != 1 {
		t.Fatalf("latest closed klines = %d, want 1", len(latest))
	}
	if latest[0].Symbol != "BTCUSDT" || latest[0].OpenTime != 2000 {
		t.Fatalf("latest[0] = %#v, want BTCUSDT open_time 2000", latest[0])
	}
}

func TestMarketStoreFlushesClickHouseBatch(t *testing.T) {
	writer := &fakeClickHouseWriter{}
	s := NewMarketStore(&RedisStore{}, nil, MarketStoreOptions{})
	s.clickhouse = writer

	s.enqueueClickHouseKline(model.Kline{Symbol: "ETHUSDT", IsClosed: true})
	s.enqueueClickHouseKline(model.Kline{Symbol: "BTCUSDT", IsClosed: true})

	if err := s.flushAllClickHouse(context.Background()); err != nil {
		t.Fatalf("flushAllClickHouse: %v", err)
	}
	if len(writer.klines) != 2 {
		t.Fatalf("written klines = %d, want 2", len(writer.klines))
	}
}

func TestMarketStoreRequeuesClickHouseBatchWhenPendingUnavailable(t *testing.T) {
	writer := &fakeClickHouseWriter{writeKlinesErr: errors.New("clickhouse unavailable")}
	s := NewMarketStore(&RedisStore{}, nil, MarketStoreOptions{})
	s.clickhouse = writer

	s.enqueueClickHouseKline(model.Kline{Symbol: "ETHUSDT", IsClosed: true})

	if err := s.flushAllClickHouse(context.Background()); err == nil {
		t.Fatal("expected flushAllClickHouse to fail")
	}
	batch := s.drainClickHouse(10)
	if len(batch.klines) != 1 {
		t.Fatalf("requeued klines = %d, want 1", len(batch.klines))
	}
}

func TestMarketStoreRequeueLatestKeepsNewerValues(t *testing.T) {
	s := NewMarketStore(&RedisStore{}, nil, MarketStoreOptions{})
	oldBatch := latestBatch{
		lastPrices: []model.LastPrice{{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Price:    "100",
		}},
		openKlines: []model.Kline{{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 1000,
			Close:    "100",
		}},
		indicators: []model.IndicatorSnapshot{{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 1000,
			Values:   map[string]string{"rsi": "50"},
		}},
		indicatorRTs: []model.IndicatorRealtimeSnapshot{{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 1000,
			Kline: model.Kline{
				Exchange: "binance",
				Market:   "um",
				Symbol:   "ETHUSDT",
				Interval: "1m",
				OpenTime: 1000,
				Close:    "100",
			},
			Values: map[string]string{"rsi": "50"},
		}},
	}
	if err := s.SetLastPrice(context.Background(), model.LastPrice{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Price:    "101",
	}); err != nil {
		t.Fatalf("SetLastPrice: %v", err)
	}
	s.latestMu.Lock()
	s.openKlines[klineLatestKey(model.Kline{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
	})] = model.Kline{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1000,
		Close:    "101",
	}
	s.indicators[model.IndicatorKey("binance", "um", "ETHUSDT", "1m")] = model.IndicatorSnapshot{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1000,
		Values:   map[string]string{"rsi": "51"},
	}
	s.indicatorRTs[model.IndicatorRealtimeKey("binance", "um", "ETHUSDT", "1m")] = model.IndicatorRealtimeSnapshot{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "1m",
		OpenTime: 1000,
		Kline: model.Kline{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 1000,
			Close:    "101",
		},
		Values: map[string]string{"rsi": "51"},
	}
	s.latestMu.Unlock()

	s.requeueLatest(oldBatch)

	batch := s.drainLatest()
	if len(batch.lastPrices) != 1 {
		t.Fatalf("last prices = %d, want 1", len(batch.lastPrices))
	}
	if batch.lastPrices[0].Price != "101" {
		t.Fatalf("last price = %q, want 101", batch.lastPrices[0].Price)
	}
	if len(batch.openKlines) != 1 {
		t.Fatalf("open klines = %d, want 1", len(batch.openKlines))
	}
	if batch.openKlines[0].Close != "101" {
		t.Fatalf("open kline close = %q, want 101", batch.openKlines[0].Close)
	}
	if len(batch.indicators) != 1 {
		t.Fatalf("indicators = %d, want 1", len(batch.indicators))
	}
	if batch.indicators[0].Values["rsi"] != "51" {
		t.Fatalf("indicator rsi = %q, want 51", batch.indicators[0].Values["rsi"])
	}
	if len(batch.indicatorRTs) != 1 {
		t.Fatalf("indicator realtime snapshots = %d, want 1", len(batch.indicatorRTs))
	}
	if batch.indicatorRTs[0].Kline.Close != "101" {
		t.Fatalf("indicator realtime close = %q, want 101", batch.indicatorRTs[0].Kline.Close)
	}
}

func TestLatestClosedKlinesKeepsNewestPerKey(t *testing.T) {
	latest := latestClosedKlines([]model.Kline{
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 1000,
			IsClosed: true,
		},
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 2000,
			IsClosed: true,
		},
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "5m",
			OpenTime: 1500,
			IsClosed: true,
		},
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "BTCUSDT",
			Interval: "1m",
			OpenTime: 1200,
			IsClosed: true,
		},
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "BTCUSDT",
			Interval: "1m",
			OpenTime: 3000,
			IsClosed: false,
		},
	})

	if len(latest) != 3 {
		t.Fatalf("latest = %d, want 3", len(latest))
	}
	byKey := map[string]model.Kline{}
	for _, kline := range latest {
		byKey[klineLatestKey(kline)] = kline
	}
	eth1m := model.RedisKey("binance", "um", "ETHUSDT", "1m")
	eth5m := model.RedisKey("binance", "um", "ETHUSDT", "5m")
	btc1m := model.RedisKey("binance", "um", "BTCUSDT", "1m")
	if byKey[eth1m].OpenTime != 2000 {
		t.Fatalf("ETHUSDT 1m open time = %d, want 2000", byKey[eth1m].OpenTime)
	}
	if byKey[eth5m].OpenTime != 1500 {
		t.Fatalf("ETHUSDT 5m open time = %d, want 1500", byKey[eth5m].OpenTime)
	}
	if byKey[btc1m].OpenTime != 1200 {
		t.Fatalf("BTCUSDT 1m open time = %d, want 1200", byKey[btc1m].OpenTime)
	}
}

func TestMarketStoreLastOpenTimeUsesMemoryCache(t *testing.T) {
	s := NewMarketStore(&RedisStore{}, nil, MarketStoreOptions{})
	s.rememberLastOpenTime(model.RedisKey("binance", "um", "ETHUSDT", "1m"), 2000)

	lastOpenTime, ok, err := s.LastOpenTime(context.Background(), "binance", "um", "ETHUSDT", "1m")
	if err != nil {
		t.Fatalf("LastOpenTime: %v", err)
	}
	if !ok {
		t.Fatal("LastOpenTime ok = false, want true")
	}
	if lastOpenTime != 2000 {
		t.Fatalf("LastOpenTime = %d, want 2000", lastOpenTime)
	}
}

func TestMarketStoreRememberLastOpenTimesKeepsLatestClosedKline(t *testing.T) {
	s := NewMarketStore(&RedisStore{}, nil, MarketStoreOptions{})
	s.rememberLastOpenTimes([]model.Kline{
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 2000,
			IsClosed: true,
		},
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 1000,
			IsClosed: true,
		},
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 3000,
			IsClosed: false,
		},
	})

	lastOpenTime, ok, err := s.LastOpenTime(context.Background(), "binance", "um", "ETHUSDT", "1m")
	if err != nil {
		t.Fatalf("LastOpenTime: %v", err)
	}
	if !ok {
		t.Fatal("LastOpenTime ok = false, want true")
	}
	if lastOpenTime != 2000 {
		t.Fatalf("LastOpenTime = %d, want 2000", lastOpenTime)
	}
}

func TestMarketStoreMarketStatusDedupesAfterSuccessfulWrite(t *testing.T) {
	s := NewMarketStore(&RedisStore{}, nil, MarketStoreOptions{})
	status := model.MarketStatus{
		Exchange:  "binance",
		Market:    "um",
		Available: true,
		Reason:    "ok",
	}

	if !s.shouldWriteMarketStatus(status) {
		t.Fatal("first status should be written")
	}
	if !s.shouldWriteMarketStatus(status) {
		t.Fatal("status should still be written before successful persistence is remembered")
	}
	s.rememberMarketStatus(status)
	if s.shouldWriteMarketStatus(status) {
		t.Fatal("unchanged remembered status should not be written")
	}
	status.Available = false
	status.Reason = "down"
	if !s.shouldWriteMarketStatus(status) {
		t.Fatal("changed status should be written")
	}
}

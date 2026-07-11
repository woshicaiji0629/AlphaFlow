package store

import (
	"alphaflow/go-service/polymarket-research/internal/model"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type batchWriter interface {
	UpsertMarkets(context.Context, []model.Market) error
	WriteBookTicks(context.Context, []model.BookTick) error
	WriteTrades(context.Context, []model.Trade) error
	WriteReferencePrices(context.Context, []model.ReferencePrice) error
	WriteResolutions(context.Context, []model.Resolution) error
	Close() error
}
type event struct {
	book       *model.BookTick
	trade      *model.Trade
	price      *model.ReferencePrice
	resolution *model.Resolution
}
type BatchStore struct {
	writer      batchWriter
	events      chan event
	max         int
	interval    time.Duration
	done        chan struct{}
	once        sync.Once
	pending     atomic.Int64
	flushErrors atomic.Int64
}

func NewBatchStore(w batchWriter, max, channelSize int, interval time.Duration) *BatchStore {
	b := &BatchStore{writer: w, events: make(chan event, channelSize), max: max, interval: interval, done: make(chan struct{})}
	go b.run()
	return b
}
func (b *BatchStore) enqueue(v event) error {
	b.pending.Add(1)
	select {
	case b.events <- v:
		return nil
	default:
		b.pending.Add(-1)
		return fmt.Errorf("polymarket batch channel full")
	}
}
func (b *BatchStore) WriteBookTick(_ context.Context, v model.BookTick) error {
	return b.enqueue(event{book: &v})
}
func (b *BatchStore) WriteTrade(_ context.Context, v model.Trade) error {
	return b.enqueue(event{trade: &v})
}
func (b *BatchStore) WriteReferencePrice(_ context.Context, v model.ReferencePrice) error {
	return b.enqueue(event{price: &v})
}
func (b *BatchStore) WriteResolution(_ context.Context, v model.Resolution) error {
	return b.enqueue(event{resolution: &v})
}
func (b *BatchStore) Stats() (int64, int64) { return b.pending.Load(), b.flushErrors.Load() }
func (b *BatchStore) UpsertMarkets(ctx context.Context, values []model.Market) error {
	return b.writer.UpsertMarkets(ctx, values)
}
func (b *BatchStore) Close() error {
	b.once.Do(func() { close(b.events); <-b.done })
	return b.writer.Close()
}
func (b *BatchStore) run() {
	defer close(b.done)
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	var books []model.BookTick
	var trades []model.Trade
	var prices []model.ReferencePrice
	var resolutions []model.Resolution
	flush := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var errors int64
		retry := func(write func() error) error {
			var last error
			for attempt := 0; attempt < 3; attempt++ {
				if last = write(); last == nil {
					return nil
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
				}
			}
			return last
		}
		if e := retry(func() error { return b.writer.WriteBookTicks(ctx, books) }); e != nil {
			errors++
			slog.Error("flush polymarket book batch failed", "error", e, "records", len(books))
		} else {
			b.pending.Add(-int64(len(books)))
			books = nil
		}
		if e := retry(func() error { return b.writer.WriteTrades(ctx, trades) }); e != nil {
			errors++
			slog.Error("flush polymarket trade batch failed", "error", e, "records", len(trades))
		} else {
			b.pending.Add(-int64(len(trades)))
			trades = nil
		}
		if e := retry(func() error { return b.writer.WriteReferencePrices(ctx, prices) }); e != nil {
			errors++
			slog.Error("flush polymarket reference price batch failed", "error", e, "records", len(prices))
		} else {
			b.pending.Add(-int64(len(prices)))
			prices = nil
		}
		if e := retry(func() error { return b.writer.WriteResolutions(ctx, resolutions) }); e != nil {
			errors++
			slog.Error("flush polymarket resolution batch failed", "error", e, "records", len(resolutions))
		} else {
			b.pending.Add(-int64(len(resolutions)))
			resolutions = nil
		}
		if errors > 0 {
			b.flushErrors.Add(errors)
		}
	}
	for {
		select {
		case v, ok := <-b.events:
			if !ok {
				flush()
				return
			}
			if v.book != nil {
				books = append(books, *v.book)
			}
			if v.trade != nil {
				trades = append(trades, *v.trade)
			}
			if v.price != nil {
				prices = append(prices, *v.price)
			}
			if v.resolution != nil {
				resolutions = append(resolutions, *v.resolution)
			}
			if len(books)+len(trades)+len(prices)+len(resolutions) >= b.max {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

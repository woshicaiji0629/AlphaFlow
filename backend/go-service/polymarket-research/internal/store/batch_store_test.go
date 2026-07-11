package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"alphaflow/go-service/polymarket-research/internal/model"
)

type fakeBatchWriter struct {
	mu           sync.Mutex
	books        int
	prices       int
	bookFailures int
}

func (f *fakeBatchWriter) UpsertMarkets(context.Context, []model.Market) error { return nil }
func (f *fakeBatchWriter) WriteBookTicks(_ context.Context, v []model.BookTick) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(v) > 0 && f.bookFailures > 0 {
		f.bookFailures--
		return errors.New("temporary failure")
	}
	f.books += len(v)
	return nil
}

func TestBatchStoreRetainsFailedBatch(t *testing.T) {
	writer := &fakeBatchWriter{bookFailures: 3}
	store := NewBatchStore(writer, 1, 10, 10*time.Millisecond)
	if err := store.WriteBookTick(context.Background(), model.BookTick{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		writer.mu.Lock()
		books := writer.books
		writer.mu.Unlock()
		if books == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.books != 1 {
		t.Fatalf("books=%d", writer.books)
	}
	if pending, failures := store.Stats(); pending != 0 || failures == 0 {
		t.Fatalf("pending=%d failures=%d", pending, failures)
	}
}
func (f *fakeBatchWriter) WriteTrades(context.Context, []model.Trade) error { return nil }
func (f *fakeBatchWriter) WriteReferencePrices(_ context.Context, v []model.ReferencePrice) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prices += len(v)
	return nil
}
func (f *fakeBatchWriter) WriteResolutions(context.Context, []model.Resolution) error { return nil }
func (f *fakeBatchWriter) Close() error                                               { return nil }

func TestBatchStoreFlushesAtMaxSize(t *testing.T) {
	writer := &fakeBatchWriter{}
	store := NewBatchStore(writer, 2, 10, time.Hour)
	if err := store.WriteBookTick(context.Background(), model.BookTick{}); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteReferencePrice(context.Background(), model.ReferencePrice{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		writer.mu.Lock()
		count := writer.books + writer.prices
		writer.mu.Unlock()
		if count == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	store.Close()
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.books != 1 || writer.prices != 1 {
		t.Fatalf("books=%d prices=%d", writer.books, writer.prices)
	}
}

func TestBatchStoreFlushesOnClose(t *testing.T) {
	writer := &fakeBatchWriter{}
	store := NewBatchStore(writer, 100, 10, time.Hour)
	if err := store.WriteBookTick(context.Background(), model.BookTick{}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.books != 1 {
		t.Fatalf("books=%d", writer.books)
	}
}

func TestBatchStoreRejectsFullChannel(t *testing.T) {
	writer := &fakeBatchWriter{}
	store := &BatchStore{writer: writer, events: make(chan event, 1), max: 100, interval: time.Hour, done: make(chan struct{})}
	store.events <- event{}
	if err := store.WriteBookTick(context.Background(), model.BookTick{}); err == nil {
		t.Fatal("expected full channel error")
	}
}

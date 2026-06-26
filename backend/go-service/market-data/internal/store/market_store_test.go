package store

import (
	"context"
	"testing"

	"alphaflow/go-service/market-data/internal/model"
)

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
	if batch := s.drainLatest(); len(batch.lastPrices) != 0 || len(batch.markPrices) != 0 || len(batch.bookTickers) != 0 {
		t.Fatalf("drainLatest after drain = %#v, want empty", batch)
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
	}
	if err := s.SetLastPrice(context.Background(), model.LastPrice{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Price:    "101",
	}); err != nil {
		t.Fatalf("SetLastPrice: %v", err)
	}

	s.requeueLatest(oldBatch)

	batch := s.drainLatest()
	if len(batch.lastPrices) != 1 {
		t.Fatalf("last prices = %d, want 1", len(batch.lastPrices))
	}
	if batch.lastPrices[0].Price != "101" {
		t.Fatalf("last price = %q, want 101", batch.lastPrices[0].Price)
	}
}

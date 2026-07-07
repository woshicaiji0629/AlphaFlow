package collector

import (
	"context"
	"errors"

	"alphaflow/go-service/market-data/internal/model"
)

func (c *Collector) HandleKline(ctx context.Context, kline model.Kline) error {
	if kline.Exchange == "" || kline.Market == "" || kline.Symbol == "" || kline.Interval == "" {
		return errors.New("invalid empty kline identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType: collectorEventKline,
		kline:     kline,
	})
}

func (c *Collector) HandleLastPrice(ctx context.Context, price model.LastPrice) error {
	if price.Exchange == "" || price.Market == "" || price.Symbol == "" {
		return errors.New("invalid empty last price identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType: collectorEventLastPrice,
		lastPrice: price,
	})
}

func (c *Collector) HandleMarkPrice(ctx context.Context, price model.MarkPrice) error {
	if price.Exchange == "" || price.Market == "" || price.Symbol == "" {
		return errors.New("invalid empty mark price identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType: collectorEventMarkPrice,
		markPrice: price,
	})
}

func (c *Collector) HandleBookTicker(ctx context.Context, ticker model.BookTicker) error {
	if ticker.Exchange == "" || ticker.Market == "" || ticker.Symbol == "" {
		return errors.New("invalid empty book ticker identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType:  collectorEventBookTicker,
		bookTicker: ticker,
	})
}

func (c *Collector) HandleOpenInterest(ctx context.Context, interest model.OpenInterest) error {
	if interest.Exchange == "" || interest.Market == "" || interest.Symbol == "" {
		return errors.New("invalid empty open interest identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType:    collectorEventOpenInterest,
		openInterest: interest,
	})
}

func (c *Collector) HandleLiquidation(ctx context.Context, liquidation model.Liquidation) error {
	if liquidation.Exchange == "" || liquidation.Market == "" || liquidation.Symbol == "" {
		return errors.New("invalid empty liquidation identity")
	}
	return c.enqueueEvent(ctx, collectorEvent{
		eventType:   collectorEventLiquidation,
		liquidation: liquidation,
	})
}

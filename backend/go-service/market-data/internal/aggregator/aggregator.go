package aggregator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type Store interface {
	LastOpenTime(
		ctx context.Context,
		exchange string,
		market string,
		symbol string,
		interval string,
	) (int64, bool, error)
	RangeKlines(
		ctx context.Context,
		exchange string,
		market string,
		symbol string,
		interval string,
		start int64,
		end int64,
	) ([]model.Kline, error)
	UpsertKline(ctx context.Context, kline model.Kline) error
	IsMarketAvailable(ctx context.Context, exchange string, market string) (bool, error)
}

type Rule struct {
	Exchange       string
	Market         string
	Symbols        []string
	SourceInterval string
	TargetInterval string
}

type Options struct {
	Rules           []Rule
	ScanInterval    time.Duration
	LookbackPeriods int64
}

type Aggregator struct {
	store   Store
	options Options
	now     func() time.Time
}

func New(store Store, options Options) *Aggregator {
	if options.ScanInterval <= 0 {
		options.ScanInterval = 10 * time.Second
	}
	if options.LookbackPeriods <= 0 {
		options.LookbackPeriods = 200
	}
	return &Aggregator{
		store:   store,
		options: options,
		now:     time.Now,
	}
}

func (a *Aggregator) Run(ctx context.Context) error {
	a.runOnceWithLogging(ctx)

	ticker := time.NewTicker(a.options.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.runOnceWithLogging(ctx)
		}
	}
}

func (a *Aggregator) RunOnce(ctx context.Context) error {
	var errs []error
	for _, rule := range a.options.Rules {
		if err := validateRule(rule); err != nil {
			errs = append(errs, err)
			continue
		}
		available, err := a.store.IsMarketAvailable(ctx, rule.Exchange, rule.Market)
		if err != nil {
			errs = append(errs, fmt.Errorf("read market status %s %s: %w", rule.Exchange, rule.Market, err))
			continue
		}
		if !available {
			slog.Warn("skip aggregation for unavailable market", "exchange", rule.Exchange, "market", rule.Market)
			continue
		}
		for _, symbol := range rule.Symbols {
			if err := a.aggregateSymbol(ctx, rule, symbol); err != nil {
				errs = append(errs, fmt.Errorf("aggregate %s %s %s %s->%s: %w",
					rule.Exchange,
					rule.Market,
					symbol,
					rule.SourceInterval,
					rule.TargetInterval,
					err,
				))
				continue
			}
		}
	}
	return errors.Join(errs...)
}

func (a *Aggregator) runOnceWithLogging(ctx context.Context) {
	if err := a.RunOnce(ctx); err != nil && ctx.Err() == nil {
		slog.Error("aggregate klines failed", "error", err)
	}
}

func (a *Aggregator) aggregateSymbol(ctx context.Context, rule Rule, symbol string) error {
	sourceMillis, err := model.IntervalMillis(rule.SourceInterval)
	if err != nil {
		return err
	}
	targetMillis, err := model.IntervalMillis(rule.TargetInterval)
	if err != nil {
		return err
	}
	if targetMillis%sourceMillis != 0 {
		return fmt.Errorf("target interval %s is not divisible by source interval %s", rule.TargetInterval, rule.SourceInterval)
	}

	start, end, ok, err := a.scanWindow(ctx, rule, symbol, targetMillis)
	if err != nil || !ok {
		return err
	}

	for openTime := start; openTime <= end; openTime += targetMillis {
		klines, err := a.store.RangeKlines(
			ctx,
			rule.Exchange,
			rule.Market,
			symbol,
			rule.SourceInterval,
			openTime,
			openTime+targetMillis-1,
		)
		if err != nil {
			return err
		}
		aggregated, ok, err := Aggregate(rule, symbol, openTime, klines)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err := a.store.UpsertKline(ctx, aggregated); err != nil {
			return err
		}
		slog.Debug(
			"aggregated kline",
			"exchange", rule.Exchange,
			"market", rule.Market,
			"symbol", symbol,
			"source_interval", rule.SourceInterval,
			"target_interval", rule.TargetInterval,
			"open_time", openTime,
		)
	}
	return nil
}

func (a *Aggregator) scanWindow(
	ctx context.Context,
	rule Rule,
	symbol string,
	targetMillis int64,
) (int64, int64, bool, error) {
	lastOpenTime, ok, err := a.store.LastOpenTime(ctx, rule.Exchange, rule.Market, symbol, rule.TargetInterval)
	if err != nil {
		return 0, 0, false, err
	}
	var start int64
	if ok {
		start = lastOpenTime + targetMillis
	} else {
		start = alignOpenTime(a.now().Add(-time.Duration(targetMillis*a.options.LookbackPeriods)*time.Millisecond).UnixMilli(), targetMillis)
	}

	end := alignOpenTime(a.now().UnixMilli(), targetMillis) - targetMillis
	if start > end {
		return 0, 0, false, nil
	}
	return start, end, true, nil
}

func Aggregate(rule Rule, symbol string, openTime int64, klines []model.Kline) (model.Kline, bool, error) {
	if err := validateRule(rule); err != nil {
		return model.Kline{}, false, err
	}
	sourceMillis, err := model.IntervalMillis(rule.SourceInterval)
	if err != nil {
		return model.Kline{}, false, err
	}
	targetMillis, err := model.IntervalMillis(rule.TargetInterval)
	if err != nil {
		return model.Kline{}, false, err
	}
	required := int(targetMillis / sourceMillis)
	if len(klines) != required {
		return model.Kline{}, false, nil
	}

	for index, kline := range klines {
		if !kline.IsClosed {
			return model.Kline{}, false, nil
		}
		wantOpenTime := openTime + int64(index)*sourceMillis
		if kline.OpenTime != wantOpenTime {
			return model.Kline{}, false, nil
		}
	}

	high, err := parseDecimal(klines[0].High)
	if err != nil {
		return model.Kline{}, false, fmt.Errorf("parse high: %w", err)
	}
	low, err := parseDecimal(klines[0].Low)
	if err != nil {
		return model.Kline{}, false, fmt.Errorf("parse low: %w", err)
	}
	volume := new(big.Rat)
	quoteVolume := new(big.Rat)
	takerBuyVolume := new(big.Rat)
	takerBuyQuoteVolume := new(big.Rat)
	var tradeCount int64
	var eventTime int64

	for _, kline := range klines {
		itemHigh, err := parseDecimal(kline.High)
		if err != nil {
			return model.Kline{}, false, fmt.Errorf("parse high: %w", err)
		}
		if itemHigh.Cmp(high) > 0 {
			high = itemHigh
		}
		itemLow, err := parseDecimal(kline.Low)
		if err != nil {
			return model.Kline{}, false, fmt.Errorf("parse low: %w", err)
		}
		if itemLow.Cmp(low) < 0 {
			low = itemLow
		}
		if err := addDecimal(volume, kline.Volume); err != nil {
			return model.Kline{}, false, fmt.Errorf("parse volume: %w", err)
		}
		if err := addDecimal(quoteVolume, kline.QuoteVolume); err != nil {
			return model.Kline{}, false, fmt.Errorf("parse quote volume: %w", err)
		}
		if err := addDecimal(takerBuyVolume, kline.TakerBuyVolume); err != nil {
			return model.Kline{}, false, fmt.Errorf("parse taker buy volume: %w", err)
		}
		if err := addDecimal(takerBuyQuoteVolume, kline.TakerBuyQuoteVolume); err != nil {
			return model.Kline{}, false, fmt.Errorf("parse taker buy quote volume: %w", err)
		}
		tradeCount += kline.TradeCount
		if kline.EventTime > eventTime {
			eventTime = kline.EventTime
		}
	}

	first := klines[0]
	last := klines[len(klines)-1]
	return model.Kline{
		Exchange:            rule.Exchange,
		Market:              rule.Market,
		Symbol:              symbol,
		Interval:            rule.TargetInterval,
		OpenTime:            openTime,
		CloseTime:           openTime + targetMillis - 1,
		Open:                first.Open,
		High:                decimalString(high),
		Low:                 decimalString(low),
		Close:               last.Close,
		Volume:              decimalString(volume),
		QuoteVolume:         decimalString(quoteVolume),
		TradeCount:          tradeCount,
		TakerBuyVolume:      decimalString(takerBuyVolume),
		TakerBuyQuoteVolume: decimalString(takerBuyQuoteVolume),
		IsClosed:            true,
		EventTime:           eventTime,
		FirstTradeID:        first.FirstTradeID,
		LastTradeID:         last.LastTradeID,
	}, true, nil
}

func validateRule(rule Rule) error {
	if rule.Exchange == "" || rule.Market == "" || rule.SourceInterval == "" || rule.TargetInterval == "" {
		return fmt.Errorf("invalid aggregation rule: %#v", rule)
	}
	return nil
}

func alignOpenTime(timestamp int64, intervalMillis int64) int64 {
	return timestamp - timestamp%intervalMillis
}

func parseDecimal(value string) (*big.Rat, error) {
	if strings.TrimSpace(value) == "" {
		return new(big.Rat), nil
	}
	number, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("invalid decimal %q", value)
	}
	return number, nil
}

func addDecimal(sum *big.Rat, value string) error {
	number, err := parseDecimal(value)
	if err != nil {
		return err
	}
	sum.Add(sum, number)
	return nil
}

func decimalString(value *big.Rat) string {
	text := value.FloatString(18)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}

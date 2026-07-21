package simulator

import (
	"strings"

	"alphaflow/go-service/backtest-engine/internal/report"
	"alphaflow/go-service/pkg/strategy"
)

type AccountConfig struct {
	InitialEquity float64
	MarginQuote   float64
	Leverage      float64
	FeeRate       float64
	RebatePct     float64
}

type simulatedPosition struct {
	size     float64
	margin   float64
	fee      float64
	rebate   float64
	entryPx  float64
	side     strategy.PositionSide
	symbol   string
	strategy string
}

type SimulatedAccount struct {
	config        AccountConfig
	balance       float64
	realizedPnL   float64
	totalFee      float64
	totalRebate   float64
	positions     map[string]simulatedPosition
	latestPrices  map[string]float64
	haltNewOrders bool
	stoppedReason string
	liquidated    bool
}

func NewSimulatedAccount(config AccountConfig) *SimulatedAccount {
	return &SimulatedAccount{
		config:       config,
		balance:      config.InitialEquity,
		positions:    map[string]simulatedPosition{},
		latestPrices: map[string]float64{},
	}
}

func (a *SimulatedAccount) CanOpen() (bool, string) {
	if a == nil {
		return true, ""
	}
	if a.liquidated {
		return false, "account_liquidated"
	}
	if a.haltNewOrders {
		return false, a.stoppedReason
	}
	required := a.config.MarginQuote + a.estimatedOpenFee()
	if required <= 0 {
		return true, ""
	}
	if a.AvailableBalance() < required {
		a.haltNewOrders = true
		a.stoppedReason = "insufficient_available_balance"
		return false, a.stoppedReason
	}
	return true, ""
}

func (a *SimulatedAccount) ApplyEvents(events []strategy.StrategyEvent) {
	if a == nil {
		return
	}
	if a.liquidated {
		return
	}
	for _, event := range events {
		if event.EventType != strategy.EventTypeOrderFilled {
			continue
		}
		if isExitFill(event) {
			a.applyExitFill(event)
			continue
		}
		a.applyEntryFill(event)
	}
}

func (a *SimulatedAccount) UpdatePriceFromContext(item strategy.Context) {
	if a == nil {
		return
	}
	snapshot, ok := item.Snapshots[item.Target.Interval]
	if !ok {
		return
	}
	price, ok := parseExecutorFloat(snapshot.Current.Close)
	if ok && price >= 0 {
		a.latestPrices[item.Target.Symbol] = price
	}
}

func (a *SimulatedAccount) Snapshot(item strategy.Context, positions []strategy.Position) (report.AccountEquityPoint, bool) {
	if a == nil {
		return report.AccountEquityPoint{}, false
	}
	snapshot, ok := item.Snapshots[item.Target.Interval]
	if !ok {
		return report.AccountEquityPoint{}, false
	}
	a.UpdatePriceFromContext(item)
	return a.SnapshotAt(snapshot.Current.OpenTime, positions), true
}

func (a *SimulatedAccount) SnapshotAt(openTime int64, positions []strategy.Position) report.AccountEquityPoint {
	return a.snapshotAtPrices(openTime, positions, a.latestPrices)
}

func (a *SimulatedAccount) SnapshotWorstBar(contexts []strategy.Context, positions []strategy.Position) (report.AccountEquityPoint, map[string]float64, bool) {
	if a == nil || len(contexts) == 0 {
		return report.AccountEquityPoint{}, nil, false
	}
	prices := make(map[string]float64, len(a.latestPrices))
	for symbol, price := range a.latestPrices {
		prices[symbol] = price
	}
	openTime := int64(0)
	for _, item := range contexts {
		snapshot, ok := item.Snapshots[item.Target.Interval]
		if !ok {
			continue
		}
		if openTime == 0 {
			openTime = snapshot.Current.OpenTime
		}
		low, lowOK := parseExecutorFloat(snapshot.Current.Low)
		high, highOK := parseExecutorFloat(snapshot.Current.High)
		if !lowOK || !highOK || low <= 0 || high < low {
			continue
		}
		lowPnL := symbolUnrealizedPnL(positions, item.Target.Symbol, low)
		highPnL := symbolUnrealizedPnL(positions, item.Target.Symbol, high)
		if lowPnL <= highPnL {
			prices[item.Target.Symbol] = low
		} else {
			prices[item.Target.Symbol] = high
		}
	}
	return a.snapshotAtPrices(openTime, positions, prices), prices, true
}

func (a *SimulatedAccount) snapshotAtPrices(openTime int64, positions []strategy.Position, prices map[string]float64) report.AccountEquityPoint {
	unrealizedPnL := 0.0
	for _, currentPosition := range positions {
		positionPrice, hasPrice := prices[currentPosition.Symbol]
		if !hasPrice {
			positionPrice, _ = parseExecutorFloat(currentPosition.EntryPrice)
		}
		unrealizedPnL += unrealizedPositionPnL(currentPosition, positionPrice)
	}
	equity := a.balance + unrealizedPnL
	point := report.AccountEquityPoint{
		Time:             openTime,
		InitialEquity:    a.config.InitialEquity,
		Balance:          a.balance,
		AvailableBalance: equity - a.UsedMargin(),
		UsedMargin:       a.UsedMargin(),
		RealizedPnL:      a.realizedPnL,
		UnrealizedPnL:    unrealizedPnL,
		Fee:              a.totalFee,
		Rebate:           a.totalRebate,
		Equity:           equity,
		ReturnPct:        accountReturnPct(a.config.InitialEquity, equity),
		Liquidated:       a.liquidated,
		StoppedReason:    a.stoppedReason,
	}
	if equity <= 0 {
		a.liquidated = true
		a.haltNewOrders = true
		a.stoppedReason = "liquidated"
		a.balance = 0
		a.positions = map[string]simulatedPosition{}
		point.Balance = 0
		point.AvailableBalance = 0
		point.UsedMargin = 0
		point.Equity = 0
		point.ReturnPct = -100
		point.Liquidated = true
		point.StoppedReason = a.stoppedReason
	}
	return point
}

func symbolUnrealizedPnL(positions []strategy.Position, symbol string, price float64) float64 {
	total := 0.0
	for _, currentPosition := range positions {
		if currentPosition.Symbol == symbol {
			total += unrealizedPositionPnL(currentPosition, price)
		}
	}
	return total
}

func (a *SimulatedAccount) UsedMargin() float64 {
	if a == nil {
		return 0
	}
	total := 0.0
	for _, item := range a.positions {
		total += item.margin
	}
	return total
}

func (a *SimulatedAccount) AvailableBalance() float64 {
	if a == nil {
		return 0
	}
	return a.balance + a.unrealizedPnL() - a.UsedMargin()
}

func (a *SimulatedAccount) Liquidated() bool {
	return a != nil && a.liquidated
}

func (a *SimulatedAccount) applyEntryFill(event strategy.StrategyEvent) {
	fee := parseEventNumber(event.Fee)
	rebate := parseEventNumber(event.Metadata["rebate"])
	pnl := parseEventNumber(event.PnL)
	margin := parseEventNumber(event.Metadata["margin_quote"])
	if margin <= 0 {
		margin = a.config.MarginQuote
	}
	entryPrice := parseEventNumber(event.Price)
	a.balance += pnl
	a.realizedPnL += pnl
	a.totalFee += fee
	a.totalRebate += rebate
	key := accountPositionKey(event)
	a.positions[key] = simulatedPosition{
		size:     event.Size,
		margin:   margin,
		fee:      fee,
		rebate:   rebate,
		entryPx:  entryPrice,
		side:     positionSideFromEvent(event),
		symbol:   event.Symbol,
		strategy: event.StrategyName,
	}
}

func (a *SimulatedAccount) applyExitFill(event strategy.StrategyEvent) {
	key := accountPositionKey(event)
	current := a.positions[key]
	releaseRatio := 1.0
	if current.size > 0 && event.Size > 0 && event.Size < current.size {
		releaseRatio = event.Size / current.size
	}
	releasedFee := current.fee * releaseRatio
	releasedRebate := current.rebate * releaseRatio
	totalExitEventFee := parseEventNumber(event.Fee)
	totalExitEventRebate := parseEventNumber(event.Metadata["rebate"])
	exitOnlyFee := totalExitEventFee - releasedFee
	if exitOnlyFee < 0 {
		exitOnlyFee = 0
	}
	exitOnlyRebate := totalExitEventRebate - releasedRebate
	if exitOnlyRebate < 0 {
		exitOnlyRebate = 0
	}
	cashflow := parseEventNumber(event.PnL)
	if grossPnL := parseEventNumber(event.Metadata["gross_pnl"]); grossPnL != 0 || event.Metadata["gross_pnl"] != "" {
		cashflow = grossPnL - exitOnlyFee
	}
	a.balance += cashflow
	a.realizedPnL += cashflow
	a.totalFee += exitOnlyFee
	a.totalRebate += exitOnlyRebate
	if current.size <= 0 || releaseRatio >= 1 {
		delete(a.positions, key)
		return
	}
	current.size -= event.Size
	current.margin -= current.margin * releaseRatio
	current.fee -= releasedFee
	current.rebate -= releasedRebate
	a.positions[key] = current
}

func (a *SimulatedAccount) estimatedOpenFee() float64 {
	notional := a.config.MarginQuote * a.config.Leverage
	if notional <= 0 || a.config.FeeRate <= 0 {
		return 0
	}
	fee := notional * a.config.FeeRate
	fee -= fee * normalizedAccountRebatePct(a.config.RebatePct) / 100
	if fee < 0 {
		return 0
	}
	return fee
}

func (a *SimulatedAccount) unrealizedPnL() float64 {
	total := 0.0
	for _, position := range a.positions {
		price, hasPrice := a.latestPrices[position.symbol]
		if !hasPrice {
			price = position.entryPx
		}
		switch position.side {
		case strategy.PositionSideLong:
			total += (price - position.entryPx) * position.size
		case strategy.PositionSideShort:
			total += (position.entryPx - price) * position.size
		}
	}
	return total
}

func accountPositionKey(event strategy.StrategyEvent) string {
	return strings.Join([]string{
		event.RunID,
		event.Account,
		event.Exchange,
		event.Market,
		event.Symbol,
		event.StrategyName,
		string(event.PositionSide),
	}, ":")
}

func positionSideFromEvent(event strategy.StrategyEvent) strategy.PositionSide {
	if event.PositionSide == strategy.ExchangePositionSideShort {
		return strategy.PositionSideShort
	}
	return strategy.PositionSideLong
}

func parseEventNumber(value string) float64 {
	parsed, ok := parseExecutorFloat(value)
	if !ok {
		return 0
	}
	return parsed
}

func accountReturnPct(initialEquity float64, equity float64) float64 {
	if initialEquity <= 0 {
		return 0
	}
	return (equity - initialEquity) / initialEquity * 100
}

func normalizedAccountRebatePct(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

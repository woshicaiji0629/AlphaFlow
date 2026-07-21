package simulator

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"alphaflow/go-service/backtest-engine/internal/report"
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/position"
	paperhandler "alphaflow/go-service/pkg/positionhandler/paper"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyroute"
)

type ExecutorOptions struct {
	Engine        *strategy.Engine
	Store         *position.MemoryStore
	ManagerConfig position.ManagerConfig
	FeeConfig     paperhandler.FeeConfig
	SizingConfig  paperhandler.SizingConfig
	AccountConfig AccountConfig
	SlippageBps   float64
	Now           func() int64
	Progress      func(processed int, total int, trades int)
	ProgressTotal int
}

type ExecutionSummary struct {
	Contexts       int
	Decisions      int
	Results        int
	Events         int
	OpenPositions  int
	OrderFills     int
	StrategyEvents []strategy.StrategyEvent
	BacktestTrades []strategy.BacktestTrade
	BarEquityCurve []report.BarEquityPoint
	AccountCurve   []report.AccountEquityPoint
	RunSummary     strategy.BacktestRunSummary
	Failures       []strategy.StrategyFailure
}

type StrategyEvaluationError struct {
	Failures []strategy.StrategyFailure
}

func (e StrategyEvaluationError) Error() string {
	if len(e.Failures) == 0 {
		return "strategy evaluation failed"
	}
	first := e.Failures[0]
	return fmt.Sprintf("strategy evaluation failed: %s: %s", first.StrategyName, first.Error)
}

type Executor struct {
	engine        *strategy.Engine
	store         *position.MemoryStore
	dispatcher    *strategyroute.Dispatcher
	manager       *position.Manager
	account       *SimulatedAccount
	progress      func(processed int, total int, trades int)
	processed     int
	progressTotal int
	setNow        func(int64)
	incremental   executionState
}

type executionState struct {
	initialized bool
	eventCursor int
	realizedPnL float64
	orderFills  int
}

func NewExecutor(options ExecutorOptions) (*Executor, error) {
	if options.Engine == nil {
		return nil, fmt.Errorf("strategy engine is required")
	}
	if options.Store == nil {
		options.Store = position.NewMemoryStore()
	}
	var setNow func(int64)
	if options.Now == nil {
		currentTime := int64(0)
		options.Now = func() int64 { return currentTime }
		setNow = func(value int64) { currentTime = value }
	}
	manager := position.NewManager(options.ManagerConfig)
	handler, err := paperhandler.New(paperhandler.Options{
		PositionManager: manager,
		PositionStore:   options.Store,
		EventStore:      options.Store,
		Broker: execution.NewPaperBrokerWithOptions(execution.PaperBrokerOptions{
			Now:         options.Now,
			SlippageBps: options.SlippageBps,
		}),
		FeeConfig:    options.FeeConfig,
		SizingConfig: options.SizingConfig,
		Now:          options.Now,
	})
	if err != nil {
		return nil, err
	}
	dispatcher, err := strategyroute.NewDispatcher(strategyroute.DispatcherOptions{
		Routes: []strategyroute.Route{{
			StrategyName: "*",
			Sink:         strategyroute.SinkBacktest,
			Enabled:      true,
		}},
		Handlers: map[strategyroute.Sink]strategyroute.ResultHandler{
			strategyroute.SinkBacktest: handler,
		},
	})
	if err != nil {
		return nil, err
	}
	var account *SimulatedAccount
	if options.AccountConfig.InitialEquity > 0 {
		account = NewSimulatedAccount(options.AccountConfig)
	}
	return &Executor{
		engine:        options.Engine,
		store:         options.Store,
		dispatcher:    dispatcher,
		manager:       manager,
		account:       account,
		progress:      options.Progress,
		progressTotal: options.ProgressTotal,
		setNow:        setNow,
	}, nil
}

func (e *Executor) Execute(ctx context.Context, contexts []strategy.Context) (ExecutionSummary, error) {
	state := executionState{}
	return e.execute(ctx, contexts, &state, true)
}

// ExecuteIncremental executes a streaming batch while retaining event-derived
// state across calls. Full event materialization remains the caller's final
// aggregation responsibility.
func (e *Executor) ExecuteIncremental(ctx context.Context, contexts []strategy.Context) (ExecutionSummary, error) {
	return e.execute(ctx, contexts, &e.incremental, false)
}

func (e *Executor) execute(
	ctx context.Context,
	contexts []strategy.Context,
	state *executionState,
	includeEvents bool,
) (ExecutionSummary, error) {
	if e == nil {
		return ExecutionSummary{}, fmt.Errorf("executor is required")
	}
	summary := ExecutionSummary{Contexts: len(contexts)}
	if !state.initialized {
		existingEvents, eventCursor := e.store.EventsSince(0)
		state.eventCursor = eventCursor
		state.realizedPnL = realizedPnLFromEvents(existingEvents)
		for _, event := range existingEvents {
			if event.EventType == strategy.EventTypeOrderFilled {
				state.orderFills++
			}
		}
		state.initialized = true
	}
	for index := 0; index < len(contexts); {
		batchEnd := nextContextBatch(contexts, index)
		batch := contexts[index:batchEnd]
		equityBatch := append([]strategy.Context(nil), batch...)
		if e.account != nil {
			e.refreshAccountPrices(batch)
			if err := e.liquidateIfNeeded(ctx, batch); err != nil {
				return ExecutionSummary{}, err
			}
		}
		for batchIndex, item := range batch {
			if err := ctx.Err(); err != nil {
				return ExecutionSummary{}, err
			}
			e.setContextTime(item)
			input, err := e.contextWithPositions(ctx, item)
			if err != nil {
				return ExecutionSummary{}, err
			}
			decision, err := e.engine.Evaluate(ctx, contextForStrategyEvaluation(input))
			if err != nil {
				return ExecutionSummary{}, err
			}
			summary.Decisions++
			if len(decision.Failures) > 0 {
				summary.Failures = append(summary.Failures, decision.Failures...)
				return summary, StrategyEvaluationError{Failures: append([]strategy.StrategyFailure(nil), decision.Failures...)}
			}
			decision, err = e.filterDecision(input, decision)
			if err != nil {
				return ExecutionSummary{}, err
			}
			summary.Results += len(decision.Results)
			if err := e.dispatcher.Dispatch(ctx, input, decision); err != nil {
				return ExecutionSummary{}, err
			}
			newEvents, nextEventCursor := e.store.EventsSince(state.eventCursor)
			state.realizedPnL += realizedPnLFromEvents(newEvents)
			for _, event := range newEvents {
				if event.EventType == strategy.EventTypeOrderFilled {
					state.orderFills++
				}
			}
			if e.account != nil {
				e.account.ApplyEvents(newEvents)
			}
			state.eventCursor = nextEventCursor
			equityItem := item
			if hasExecutionFill(item, newEvents) {
				equityItem = executionAlignedContext(item)
				equityBatch[batchIndex] = equityItem
			}
			point, ok, err := e.barEquityPoint(ctx, equityItem, state.realizedPnL)
			if err != nil {
				return ExecutionSummary{}, err
			}
			if ok {
				summary.BarEquityCurve = append(summary.BarEquityCurve, point)
			}
			if e.progress != nil {
				e.processed++
				total := e.progressTotal
				if total <= 0 {
					total = e.processed + len(contexts) - summary.Decisions
				}
				e.progress(e.processed, total, summary.OrderFills)
			}
		}
		if e.account != nil {
			e.refreshAccountPrices(equityBatch)
		}
		accountPoint, ok, err := e.accountEquityPoint(ctx, equityBatch[0])
		if err != nil {
			return ExecutionSummary{}, err
		}
		if ok {
			summary.AccountCurve = append(summary.AccountCurve, accountPoint)
			if e.account.Liquidated() {
				if err := e.clearBacktestPositions(ctx, batch[0].Target); err != nil {
					return ExecutionSummary{}, err
				}
			}
		}
		index = batchEnd
	}
	summary.Events = state.eventCursor
	summary.OrderFills = state.orderFills
	if includeEvents {
		summary.StrategyEvents = e.store.Events()
	}
	runID := firstRunID(contexts)
	if runID != "" {
		positions, err := e.store.ListPositions(ctx, position.Filter{
			Scope: strategy.PositionScopeBacktest,
			RunID: runID,
		})
		if err != nil {
			return ExecutionSummary{}, err
		}
		summary.OpenPositions = len(positions)
	}
	return summary, nil
}

func executionAlignedContext(item strategy.Context) strategy.Context {
	snapshot, ok := item.Snapshots[item.Target.Interval]
	if !ok || snapshot.Execution == nil || snapshot.Execution.Price.LastPrice == "" || snapshot.Execution.Time <= 0 {
		return item
	}
	aligned := item
	aligned.Snapshots = make(map[string]strategy.Snapshot, len(item.Snapshots))
	for interval, current := range item.Snapshots {
		aligned.Snapshots[interval] = current
	}
	snapshot.Current.Close = snapshot.Execution.Price.LastPrice
	snapshot.Current.OpenTime = snapshot.Execution.Time
	snapshot.Current.CloseTime = snapshot.Execution.Time
	aligned.Snapshots[item.Target.Interval] = snapshot
	return aligned
}

func hasExecutionFill(item strategy.Context, events []strategy.StrategyEvent) bool {
	snapshot, ok := item.Snapshots[item.Target.Interval]
	if !ok || snapshot.Execution == nil || snapshot.Execution.Time <= 0 {
		return false
	}
	for _, event := range events {
		if event.EventType == strategy.EventTypeOrderFilled &&
			event.Symbol == item.Target.Symbol &&
			event.BarOpenTime == snapshot.Current.OpenTime &&
			event.EventTime == snapshot.Execution.Time {
			return true
		}
	}
	return false
}

func contextForStrategyEvaluation(input strategy.Context) strategy.Context {
	if input.Target.Scope != strategy.PositionScopeBacktest {
		return input
	}
	filtered := input
	filtered.Snapshots = make(map[string]strategy.Snapshot, len(input.Snapshots))
	for interval, snapshot := range input.Snapshots {
		snapshot.Execution = nil
		filtered.Snapshots[interval] = snapshot
	}
	return filtered
}

func (e *Executor) setContextTime(item strategy.Context) {
	if e.setNow == nil {
		return
	}
	snapshot, ok := item.Snapshots[item.Target.Interval]
	if !ok {
		e.setNow(0)
		return
	}
	eventTime := snapshot.AsOf
	if eventTime <= 0 {
		eventTime = snapshot.Current.CloseTime
	}
	e.setNow(eventTime)
}

func (e *Executor) filterDecision(input strategy.Context, decision strategy.Decision) (strategy.Decision, error) {
	if e.account == nil {
		return decision, nil
	}
	results := make([]strategy.Result, 0, len(decision.Results))
	for _, result := range decision.Results {
		plan := e.manager.PlanWithPrice(result, input.Positions[result.StrategyName], executorCurrentPrice(input))
		if isOpenAction(plan.Action) {
			ok, _ := e.account.CanOpen()
			if !ok {
				continue
			}
		}
		results = append(results, result)
	}
	decision.Results = results
	return decision, nil
}

func isOpenAction(action strategy.PositionAction) bool {
	return action == strategy.PositionActionOpenLong || action == strategy.PositionActionOpenShort
}

func nextContextBatch(contexts []strategy.Context, start int) int {
	if start >= len(contexts) {
		return start
	}
	openTime := executorContextOpenTime(contexts[start])
	end := start + 1
	for end < len(contexts) && executorContextOpenTime(contexts[end]) == openTime {
		end++
	}
	return end
}

func executorContextOpenTime(item strategy.Context) int64 {
	snapshot, ok := item.Snapshots[item.Target.Interval]
	if !ok {
		return 0
	}
	return snapshot.Current.OpenTime
}

func (e *Executor) refreshAccountPrices(contexts []strategy.Context) {
	if e.account == nil {
		return
	}
	for _, item := range contexts {
		e.account.UpdatePriceFromContext(item)
	}
}

func (e *Executor) liquidateIfNeeded(ctx context.Context, batch []strategy.Context) error {
	if e.account == nil {
		return nil
	}
	if len(batch) == 0 {
		return nil
	}
	item := batch[0]
	positions, err := e.store.ListPositions(ctx, position.Filter{
		Scope:    item.Target.Scope,
		RunID:    item.Target.RunID,
		Account:  item.Target.Account,
		Exchange: item.Target.Exchange,
		Market:   item.Target.Market,
	})
	if err != nil {
		return err
	}
	point, liquidationPrices, ok := e.account.SnapshotWorstBar(batch, positions)
	if !ok {
		return nil
	}
	if point.Liquidated {
		if err := e.appendLiquidationEvents(ctx, batch, positions, liquidationPrices); err != nil {
			return err
		}
		return e.clearBacktestPositions(ctx, item.Target)
	}
	return nil
}

func (e *Executor) appendLiquidationEvents(ctx context.Context, batch []strategy.Context, positions []strategy.Position, liquidationPrices map[string]float64) error {
	snapshots := make(map[string]strategy.Snapshot, len(batch))
	for _, item := range batch {
		if snapshot, ok := item.Snapshots[item.Target.Interval]; ok {
			snapshots[item.Target.Symbol] = snapshot
		}
	}
	events := make([]strategy.StrategyEvent, 0, len(positions))
	for _, currentPosition := range positions {
		snapshot, ok := snapshots[currentPosition.Symbol]
		if !ok {
			continue
		}
		priceValue, ok := liquidationPrices[currentPosition.Symbol]
		if !ok || priceValue <= 0 {
			priceValue, ok = liquidationPrice(currentPosition, snapshot)
		}
		entryPrice, entryOK := parseExecutorFloat(currentPosition.EntryPrice)
		if !ok || !entryOK || currentPosition.Size <= 0 {
			continue
		}
		grossPnL := (priceValue - entryPrice) * currentPosition.Size
		if currentPosition.Side == strategy.PositionSideShort {
			grossPnL = (entryPrice - priceValue) * currentPosition.Size
		}
		entryFee := liquidationFee(entryPrice*currentPosition.Size, e.account.config)
		exitFee := liquidationFee(priceValue*currentPosition.Size, e.account.config)
		eventTime := snapshot.Current.CloseTime
		if eventTime <= 0 {
			eventTime = snapshot.AsOf
		}
		events = append(events, strategy.StrategyEvent{
			EventID:      strings.Join([]string{string(currentPosition.Scope), currentPosition.RunID, currentPosition.Exchange, currentPosition.Market, currentPosition.Symbol, currentPosition.StrategyName, "order_filled", strconv.FormatInt(snapshot.Current.OpenTime, 10), "liquidation"}, ":"),
			Scope:        currentPosition.Scope,
			RunID:        currentPosition.RunID,
			Account:      currentPosition.Account,
			Exchange:     currentPosition.Exchange,
			Market:       currentPosition.Market,
			Symbol:       currentPosition.Symbol,
			StrategyName: currentPosition.StrategyName,
			EventType:    strategy.EventTypeOrderFilled,
			EventTime:    eventTime,
			BarOpenTime:  snapshot.Current.OpenTime,
			PositionSide: currentPosition.PositionSide,
			Size:         currentPosition.Size,
			Price:        formatExecutorFloat(priceValue),
			Notional:     formatExecutorFloat(priceValue * currentPosition.Size),
			Fee:          formatExecutorFloat(entryFee + exitFee),
			PnL:          formatExecutorFloat(grossPnL - entryFee - exitFee),
			Reason:       "liquidation",
			Metadata: map[string]string{
				"exit_reason": "liquidation",
				"gross_pnl":   formatExecutorFloat(grossPnL),
				"cashflow":    formatExecutorFloat(grossPnL - exitFee),
			},
			CreatedAt: eventTime,
		})
	}
	return e.store.AppendEvents(ctx, events)
}

func liquidationPrice(currentPosition strategy.Position, snapshot strategy.Snapshot) (float64, bool) {
	value := snapshot.Current.Low
	if currentPosition.Side == strategy.PositionSideShort {
		value = snapshot.Current.High
	}
	price, ok := parseExecutorFloat(value)
	if ok && price > 0 {
		return price, true
	}
	return parseExecutorFloat(snapshot.Current.Close)
}

func liquidationFee(notional float64, config AccountConfig) float64 {
	fee := notional * config.FeeRate
	fee -= fee * normalizedAccountRebatePct(config.RebatePct) / 100
	return math.Max(0, fee)
}

func formatExecutorFloat(value float64) string {
	value = math.Round(value*1e12) / 1e12
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func (e *Executor) barEquityPoint(
	ctx context.Context,
	item strategy.Context,
	realizedPnL float64,
) (report.BarEquityPoint, bool, error) {
	snapshot, ok := item.Snapshots[item.Target.Interval]
	if !ok {
		return report.BarEquityPoint{}, false, nil
	}
	price, ok := parseExecutorFloat(snapshot.Current.Close)
	if !ok {
		return report.BarEquityPoint{}, false, nil
	}
	positions, err := e.store.ListPositions(ctx, position.Filter{
		Scope:    item.Target.Scope,
		RunID:    item.Target.RunID,
		Account:  item.Target.Account,
		Exchange: item.Target.Exchange,
		Market:   item.Target.Market,
		Symbol:   item.Target.Symbol,
	})
	if err != nil {
		return report.BarEquityPoint{}, false, err
	}
	unrealizedPnL := 0.0
	for _, currentPosition := range positions {
		unrealizedPnL += unrealizedPositionPnL(currentPosition, price)
	}
	return report.BarEquityPoint{
		Time:          snapshot.Current.OpenTime,
		Symbol:        item.Target.Symbol,
		Price:         price,
		RealizedPnL:   realizedPnL,
		UnrealizedPnL: unrealizedPnL,
		Equity:        realizedPnL + unrealizedPnL,
	}, true, nil
}

func (e *Executor) accountEquityPoint(
	ctx context.Context,
	item strategy.Context,
) (report.AccountEquityPoint, bool, error) {
	if e.account == nil {
		return report.AccountEquityPoint{}, false, nil
	}
	positions, err := e.store.ListPositions(ctx, position.Filter{
		Scope:    item.Target.Scope,
		RunID:    item.Target.RunID,
		Account:  item.Target.Account,
		Exchange: item.Target.Exchange,
		Market:   item.Target.Market,
	})
	if err != nil {
		return report.AccountEquityPoint{}, false, err
	}
	point, ok := e.account.Snapshot(item, positions)
	return point, ok, nil
}

func (e *Executor) clearBacktestPositions(ctx context.Context, target strategy.Target) error {
	positions, err := e.store.ListPositions(ctx, position.Filter{
		Scope:    target.Scope,
		RunID:    target.RunID,
		Account:  target.Account,
		Exchange: target.Exchange,
		Market:   target.Market,
	})
	if err != nil {
		return err
	}
	for _, currentPosition := range positions {
		if err := e.store.DeletePosition(ctx, position.KeyFromPosition(currentPosition)); err != nil {
			return err
		}
	}
	return nil
}

func realizedPnLFromEvents(events []strategy.StrategyEvent) float64 {
	total := 0.0
	for _, event := range events {
		if event.EventType != strategy.EventTypeOrderFilled {
			continue
		}
		value, ok := parseExecutorFloat(event.Metadata["cashflow"])
		if !ok {
			value, ok = parseExecutorFloat(event.PnL)
		}
		if ok {
			total += value
		}
	}
	return total
}

func unrealizedPositionPnL(currentPosition strategy.Position, price float64) float64 {
	if currentPosition.IsFlat() {
		return 0
	}
	entryPrice, ok := parseExecutorFloat(currentPosition.EntryPrice)
	if !ok {
		return 0
	}
	switch currentPosition.Side {
	case strategy.PositionSideLong:
		return (price - entryPrice) * currentPosition.Size
	case strategy.PositionSideShort:
		return (entryPrice - price) * currentPosition.Size
	default:
		return 0
	}
}

func parseExecutorFloat(value string) (float64, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func executorCurrentPrice(input strategy.Context) string {
	snapshot, ok := input.Snapshots[input.Target.Interval]
	if !ok {
		return ""
	}
	if snapshot.Price.LastPrice != "" {
		return snapshot.Price.LastPrice
	}
	if snapshot.Price.MarkPrice != "" {
		return snapshot.Price.MarkPrice
	}
	return snapshot.Current.Close
}

func (e *Executor) contextWithPositions(ctx context.Context, item strategy.Context) (strategy.Context, error) {
	positions := make(map[string]*strategy.Position, len(e.engine.Strategies()))
	for _, itemStrategy := range e.engine.Strategies() {
		currentPosition, err := e.store.GetPosition(ctx, position.Key{
			Scope:        item.Target.Scope,
			RunID:        item.Target.RunID,
			Account:      item.Target.Account,
			Exchange:     item.Target.Exchange,
			Market:       item.Target.Market,
			Symbol:       item.Target.Symbol,
			StrategyName: itemStrategy.Name(),
			PositionSide: strategy.ExchangePositionSideNet,
		})
		if err != nil {
			return strategy.Context{}, fmt.Errorf("read position for strategy %s: %w", itemStrategy.Name(), err)
		}
		positions[itemStrategy.Name()] = currentPosition
	}
	item.Positions = positions
	return item, nil
}

func firstRunID(contexts []strategy.Context) string {
	for _, item := range contexts {
		if item.Target.RunID != "" {
			return item.Target.RunID
		}
	}
	return ""
}

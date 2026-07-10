package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/execution-engine/internal/config"
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
	"alphaflow/go-service/pkg/executionadapter"
	"github.com/redis/go-redis/v9"
)

type trackedBroker struct {
	router *brokerRouter
	redis  *redis.Client
}

func (b trackedBroker) Execute(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, error) {
	clientID, err := b.router.ClientOrderID(intent)
	if err != nil {
		return execution.ExecutionReport{}, err
	}
	key := clientIntentKey(intent.Exchange, intent.Account, clientID)
	if err := b.redis.Set(ctx, key, intent.IntentID, 7*24*time.Hour).Err(); err != nil {
		return execution.ExecutionReport{}, err
	}
	report, err := b.router.Execute(ctx, intent)
	if err != nil {
		return execution.ExecutionReport{}, err
	}
	if report.ExchangeOrderID != "" {
		if err := b.redis.Set(ctx, orderIntentKey(intent.Exchange, intent.Account, report.ExchangeOrderID), intent.IntentID, 7*24*time.Hour).Err(); err != nil {
			return execution.ExecutionReport{}, err
		}
	}
	return report, nil
}
func (b trackedBroker) Recover(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	return b.router.Recover(ctx, intent)
}
func clientIntentKey(exchange, account, clientID string) string {
	return fmt.Sprintf("execution-engine:client:%s:%s:%s", exchange, account, clientID)
}
func orderIntentKey(exchange, account, orderID string) string {
	return fmt.Sprintf("execution-engine:order:%s:%s:%s", exchange, account, orderID)
}

type privateEventHandler struct {
	redis *redis.Client
	bus   bus
	store execution.IntentStore
}

func (h privateEventHandler) Handle(ctx context.Context, event executionadapter.PrivateEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	stateKey := fmt.Sprintf("execution-engine:private:%s:%s:%s", event.Exchange, event.Account, event.Type)
	if event.Position != nil {
		stateKey += ":" + event.Position.Symbol + ":" + string(event.Position.PositionSide)
	} else if event.Order != nil {
		stateKey += ":" + event.Order.OrderID
	} else if event.Report != nil {
		stateKey += ":" + event.Report.ExchangeOrderID
	}
	applied, err := writeNewerState(ctx, h.redis, stateKey, payload, event.Sequence, event.UpdatedAt)
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	if event.Report == nil {
		return nil
	}
	lookupKey := ""
	if event.Order != nil && event.Order.ClientOrderID != "" {
		lookupKey = clientIntentKey(event.Exchange, event.Account, event.Order.ClientOrderID)
	} else if event.Report.ExchangeOrderID != "" {
		lookupKey = orderIntentKey(event.Exchange, event.Account, event.Report.ExchangeOrderID)
	}
	if lookupKey == "" {
		return nil
	}
	intentID, err := h.redis.Get(ctx, lookupKey).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	report := *event.Report
	report.IntentID = intentID
	record, err := h.store.GetIntent(ctx, intentID)
	if err != nil {
		return err
	}
	if record == nil {
		return nil
	}
	record.Report = report
	record.UpdatedAt = report.UpdatedAt
	if report.Status == execution.ExecutionStatusFilled {
		record.State = execution.IntentStateFilled
	} else if report.Status == execution.ExecutionStatusRejected {
		record.State = execution.IntentStateRejected
	}
	if err := h.store.SaveIntent(ctx, *record); err != nil {
		return err
	}
	return h.bus.PublishReport(ctx, report)
}

var newerStateScript = redis.NewScript(`local old=redis.call('GET',KEYS[2]);local incoming=tonumber(ARGV[2]);if old and tonumber(old)>=incoming then return 0 end;redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[3]);redis.call('SET',KEYS[2],ARGV[2],'PX',ARGV[3]);return 1`)

func writeNewerState(ctx context.Context, client *redis.Client, key string, payload []byte, sequence, updated int64) (bool, error) {
	version := updated
	if version == 0 {
		version = sequence
	}
	result, err := newerStateScript.Run(ctx, client, []string{key, key + ":version"}, payload, version, int64((24*time.Hour)/time.Millisecond)).Int()
	return result == 1, err
}

func runReconciliation(ctx context.Context, adapter executionadapter.Adapter, symbols []string, sink executionadapter.PrivateEventSink) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		snapshot, err := adapter.Account(ctx)
		if err != nil {
			return err
		}
		if err := sink(ctx, executionadapter.PrivateEvent{Type: executionadapter.PrivateEventAccount, Exchange: snapshot.Exchange, Account: snapshot.Account, UpdatedAt: snapshot.UpdatedAt, Snapshot: &snapshot}); err != nil {
			return err
		}
		positions, err := adapter.Positions(ctx)
		if err != nil {
			return err
		}
		for i := range positions {
			if err := sink(ctx, executionadapter.PrivateEvent{Type: executionadapter.PrivateEventPosition, Exchange: positions[i].Exchange, Account: positions[i].Account, UpdatedAt: positions[i].UpdatedAt, Position: &positions[i]}); err != nil {
				return err
			}
		}
		orderSymbols := symbols
		if len(orderSymbols) == 0 {
			orderSymbols = []string{""}
		}
		for _, symbol := range orderSymbols {
			orders, err := adapter.OpenOrders(ctx, symbol)
			if err != nil {
				if symbol == "" {
					slog.Warn("execution open-order reconciliation requires configured symbols", "error", err)
				} else {
					return err
				}
				continue
			}
			for i := range orders {
				if err := sink(ctx, executionadapter.PrivateEvent{Type: executionadapter.PrivateEventOrder, Exchange: orders[i].Exchange, Account: orders[i].Account, UpdatedAt: orders[i].UpdatedAt, Order: &orders[i]}); err != nil {
					return err
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

type accountRuntime struct {
	adapter executionadapter.Adapter
	symbols []string
	config  config.Account
	account executionaccount.Account
}

func startAccountStates(ctx context.Context, runtimes []accountRuntime, sink executionadapter.PrivateEventSink) {
	for _, runtime := range runtimes {
		runtime := runtime
		go func() {
			_ = executionadapter.RunPrivateStream(ctx, func(runCtx context.Context) error {
				return runReconciliation(runCtx, runtime.adapter, runtime.symbols, sink)
			}, executionadapter.PrivateStreamOptions{MinBackoff: time.Second, MaxBackoff: 30 * time.Second})
		}()
		if streamer, ok := runtime.adapter.(executionadapter.PrivateStreamer); ok {
			go func() {
				if err := streamer.StreamPrivate(ctx, sink); err != nil && ctx.Err() == nil {
					slog.Warn("execution private websocket unavailable; REST reconciliation remains active", "error", err)
				}
			}()
		}
	}
}

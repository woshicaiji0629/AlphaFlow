package strategyroute

import (
	"context"
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestDispatcherDispatchesMatchingRoutes(t *testing.T) {
	handler := &captureHandler{}
	dispatcher, err := NewDispatcher(DispatcherOptions{
		Routes: []Route{
			{StrategyName: "supertrend", Sink: SinkPaper, Enabled: true},
			{StrategyName: "other", Sink: SinkPaper, Enabled: true},
			{StrategyName: "supertrend", Sink: SinkLog, Enabled: false},
		},
		Handlers: map[Sink]ResultHandler{SinkPaper: handler},
	})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	err = dispatcher.Dispatch(context.Background(), strategy.Context{}, strategy.Decision{
		Results: []strategy.Result{{StrategyName: "supertrend"}},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if handler.count != 1 {
		t.Fatalf("handler count = %d, want 1", handler.count)
	}
}

func TestDispatcherRequiresHandlerForEnabledRoute(t *testing.T) {
	dispatcher, err := NewDispatcher(DispatcherOptions{
		Routes: []Route{{StrategyName: "supertrend", Sink: SinkPaper, Enabled: true}},
	})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	err = dispatcher.Dispatch(context.Background(), strategy.Context{}, strategy.Decision{
		Results: []strategy.Result{{StrategyName: "supertrend"}},
	})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want missing handler error")
	}
}

func TestParseSinkRejectsUnsupportedSink(t *testing.T) {
	if _, err := ParseSink("unknown"); err == nil {
		t.Fatal("ParseSink() error = nil, want unsupported sink error")
	}
}

type captureHandler struct {
	count int
}

func (h *captureHandler) HandleResult(ctx context.Context, input strategy.Context, result strategy.Result, route Route) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.count++
	return nil
}

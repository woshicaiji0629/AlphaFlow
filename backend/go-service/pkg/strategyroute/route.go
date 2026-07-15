package strategyroute

import (
	"context"
	"fmt"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

type Sink string

const (
	SinkPaper    Sink = "paper"
	SinkBacktest Sink = "backtest"
	SinkTestnet  Sink = "testnet"
	SinkLive     Sink = "live"
	SinkNotify   Sink = "notify"
	SinkLog      Sink = "log"
)

type Route struct {
	StrategyName string
	Sink         Sink
	Account      string
	RunID        string
	Notifier     string
	Enabled      bool
}

type ResultHandler interface {
	HandleResult(ctx context.Context, input strategy.Context, result strategy.Result, route Route) error
}

type Dispatcher struct {
	routes   []Route
	handlers map[Sink]ResultHandler
}

type DispatcherOptions struct {
	Routes   []Route
	Handlers map[Sink]ResultHandler
}

func NewDispatcher(options DispatcherOptions) (*Dispatcher, error) {
	handlers := map[Sink]ResultHandler{}
	for sink, handler := range options.Handlers {
		if !ValidSink(sink) {
			return nil, fmt.Errorf("unsupported route sink %q", sink)
		}
		if handler == nil {
			return nil, fmt.Errorf("handler for sink %q is nil", sink)
		}
		handlers[sink] = handler
	}
	routes := make([]Route, 0, len(options.Routes))
	for _, route := range options.Routes {
		route.StrategyName = strings.TrimSpace(route.StrategyName)
		if route.StrategyName == "" {
			return nil, fmt.Errorf("route strategy cannot be empty")
		}
		if !ValidSink(route.Sink) {
			return nil, fmt.Errorf("unsupported route sink %q", route.Sink)
		}
		routes = append(routes, route)
	}
	return &Dispatcher{
		routes:   routes,
		handlers: handlers,
	}, nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, input strategy.Context, decision strategy.Decision) error {
	if d == nil {
		return nil
	}
	for _, result := range decision.Results {
		for _, route := range d.matchingRoutes(result.StrategyName) {
			handler := d.handlers[route.Sink]
			if handler == nil {
				return fmt.Errorf("handler for sink %q is not configured", route.Sink)
			}
			if err := handler.HandleResult(ctx, input, result, route); err != nil {
				return fmt.Errorf("handle strategy %s route %s: %w", result.StrategyName, route.Sink, err)
			}
		}
	}
	return nil
}

func (d *Dispatcher) DispatchToSink(ctx context.Context, input strategy.Context, decision strategy.Decision, sink Sink) error {
	if d == nil {
		return nil
	}
	for _, result := range decision.Results {
		for _, route := range d.matchingRoutes(result.StrategyName) {
			if route.Sink != sink {
				continue
			}
			handler := d.handlers[route.Sink]
			if handler == nil {
				return fmt.Errorf("handler for sink %q is not configured", route.Sink)
			}
			if err := handler.HandleResult(ctx, input, result, route); err != nil {
				return fmt.Errorf("handle strategy %s route %s: %w", result.StrategyName, route.Sink, err)
			}
		}
	}
	return nil
}

func (d *Dispatcher) matchingRoutes(strategyName string) []Route {
	if d == nil {
		return nil
	}
	routes := []Route{}
	for _, route := range d.routes {
		if !route.Enabled {
			continue
		}
		if route.StrategyName == strategyName || route.StrategyName == "*" {
			routes = append(routes, route)
		}
	}
	return routes
}

func ParseSink(value string) (Sink, error) {
	sink := Sink(strings.ToLower(strings.TrimSpace(value)))
	if !ValidSink(sink) {
		return "", fmt.Errorf("unsupported route sink %q", value)
	}
	return sink, nil
}

func ValidSink(sink Sink) bool {
	switch sink {
	case SinkPaper, SinkBacktest, SinkTestnet, SinkLive, SinkNotify, SinkLog:
		return true
	default:
		return false
	}
}

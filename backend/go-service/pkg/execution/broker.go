package execution

import "context"

type Broker interface {
	Execute(ctx context.Context, intent OrderIntent) (ExecutionReport, error)
}

type RecoverableBroker interface {
	Broker
	Recover(ctx context.Context, intent OrderIntent) (ExecutionReport, bool, error)
}

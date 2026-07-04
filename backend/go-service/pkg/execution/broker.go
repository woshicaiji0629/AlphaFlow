package execution

import "context"

type Broker interface {
	Execute(ctx context.Context, intent OrderIntent) (ExecutionReport, error)
}

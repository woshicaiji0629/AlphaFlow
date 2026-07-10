package executionadapter

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

type PrivateStreamOptions struct {
	MinBackoff time.Duration
	MaxBackoff time.Duration
}

// RunPrivateStream keeps a private stream alive until ctx is canceled. Every
// successful reconnect must authenticate and subscribe again inside connect.
func RunPrivateStream(ctx context.Context, connect func(context.Context) error, options PrivateStreamOptions) error {
	if options.MinBackoff <= 0 {
		options.MinBackoff = time.Second
	}
	if options.MaxBackoff < options.MinBackoff {
		options.MaxBackoff = 30 * time.Second
	}
	backoff := options.MinBackoff
	for {
		err := connect(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return errors.New("private stream ended unexpectedly")
		}
		jitter := time.Duration(rand.Int64N(int64(backoff/2 + 1)))
		timer := time.NewTimer(backoff + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff *= 2
		if backoff > options.MaxBackoff {
			backoff = options.MaxBackoff
		}
	}
}

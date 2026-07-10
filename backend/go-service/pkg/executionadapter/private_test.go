package executionadapter

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPrivateStreamReconnectsUntilCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	err := RunPrivateStream(ctx, func(context.Context) error {
		if calls.Add(1) == 3 {
			cancel()
		}
		return errors.New("disconnected")
	}, PrivateStreamOptions{MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
	if err != nil || calls.Load() != 3 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}

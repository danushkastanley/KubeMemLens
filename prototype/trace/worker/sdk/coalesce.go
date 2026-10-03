package sdk

import (
	"context"
	"time"
)

const fileCoalesceWindow = time.Millisecond

// A notified first record can race its producer's next reservation. Briefly
// retaining the following records in the ring amortises adaptive notifications.
// This runs only after reading from an empty file ring, never during idle waits
// or while draining an existing backlog. No additional event queue is allocated.
func coalesceFileStart(ctx context.Context, validation time.Time) error {
	delay := coalesceDelay(time.Now(), validation)
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	return ctx.Err()
}

func coalesceDelay(now, validation time.Time) time.Duration {
	return min(fileCoalesceWindow, max(0, validation.Sub(now)))
}

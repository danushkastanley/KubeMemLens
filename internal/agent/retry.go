package agent

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

type retryPolicy struct {
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	jitter      func(time.Duration) time.Duration
	wait        func(context.Context, time.Duration) error
}

func defaultRetryPolicy() retryPolicy {
	return retryPolicy{
		maxAttempts: 4,
		baseDelay:   100 * time.Millisecond,
		maxDelay:    time.Second,
		jitter:      boundedJitter,
		wait:        waitForRetry,
	}
}

func (p retryPolicy) canRetry(attempt int) bool {
	return attempt < p.maxAttempts
}

func (p retryPolicy) waitBeforeRetry(ctx context.Context, attempt int) error {
	delay := p.baseDelay
	for step := 1; step < attempt && delay < p.maxDelay; step++ {
		if delay > p.maxDelay/2 {
			delay = p.maxDelay
			break
		}
		delay *= 2
	}
	if delay > p.maxDelay {
		delay = p.maxDelay
	}
	if p.jitter != nil {
		delay = p.jitter(delay)
	}
	return p.wait(ctx, delay)
}

func boundedJitter(maximum time.Duration) time.Duration {
	if maximum <= time.Nanosecond {
		return maximum
	}
	minimum := maximum / 2
	return minimum + time.Duration(rand.Int64N(int64(maximum-minimum)+1))
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func transientHTTPFailure(status int, err error) bool {
	var transportErr *requestTransportError
	if errors.As(err, &transportErr) {
		return true
	}
	return status == 429 || status >= 500
}

type requestTransportError struct {
	err error
}

func (e *requestTransportError) Error() string {
	return e.err.Error()
}

func (e *requestTransportError) Unwrap() error {
	return e.err
}

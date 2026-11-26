package workerprocess

import (
	"context"
	"testing"
	"time"
)

func TestExpiryUsesTheExpiredContextDeadline(t *testing.T) {
	// Replay a deadline notification after the wall clock has moved backwards:
	// the context's timer has expired although its serialised UTC deadline now
	// appears to be in the future. No machine clock is changed by this test.
	future := time.Now().UTC().Add(time.Hour)
	past := time.Now().UTC().Add(-time.Hour)
	for _, tc := range []struct {
		name                 string
		err                  error
		effective, requested time.Time
		want                 bool
	}{
		{"own deadline before wall clock", context.DeadlineExceeded, future, future, true},
		{"own deadline after wall clock", context.DeadlineExceeded, past, past, true},
		{"earlier parent before wall clock", context.DeadlineExceeded, future.Add(-time.Second), future, false},
		{"earlier parent observed late", context.DeadlineExceeded, past.Add(-time.Second), past, false},
		{"explicit cancellation after deadline", context.Canceled, past, past, false},
		{"no expiry", nil, past, past, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := requestDeadlineExpired(tc.err, tc.effective, tc.requested); got != tc.want {
				t.Fatalf("expiry classification=%t, want %t", got, tc.want)
			}
		})
	}
}

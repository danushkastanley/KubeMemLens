package nodebinding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracesession"
)

func TestStreamLeaseExpiryPreservesDeadlineSemantics(t *testing.T) {
	execution, expire := context.WithCancelCause(context.Background())
	defer expire(context.Canceled)
	// Deliver the lease's expiry before the stream timer. This deterministically
	// exercises the ordering that loses terminal counts when expiry is forwarded
	// through CancelCause. The absolute stream deadline must remain authoritative.
	expire(admission.ErrExpired)
	deadline := time.Now().Add(20 * time.Millisecond)
	scope, stop := streamContext(execution, deadline)
	defer stop()
	<-scope.Done()
	if scope.Err() != context.DeadlineExceeded || time.Now().Before(deadline) ||
		context.Cause(scope) != tracesession.Stop(trace.Expired) {
		t.Fatal("lease expiry became cancellation or changed the observation deadline")
	}
}

func TestStreamLeaseFailuresCancelBeforeDeadline(t *testing.T) {
	for _, tc := range []struct {
		cause error
		want  trace.Termination
	}{
		{admission.ErrDenied, trace.AuthorisationLost},
		{admission.ErrTargetChanged, trace.TargetChanged},
		{context.Canceled, trace.Cancelled},
		{admission.ErrUnavailable, trace.EngineFailed},
		{errors.New("unclassified failure"), trace.EngineFailed},
	} {
		t.Run(string(tc.want), func(t *testing.T) {
			execution, cancel := context.WithCancelCause(context.Background())
			defer cancel(context.Canceled)
			scope, stop := streamContext(execution, time.Now().Add(time.Hour))
			defer stop()
			cancel(tc.cause)
			select {
			case <-scope.Done():
			case <-time.After(time.Second):
				t.Fatal("lease failure did not stop the stream")
			}
			if scope.Err() != context.Canceled || context.Cause(scope) != tracesession.Stop(tc.want) {
				t.Fatal("lease failure lost its cancellation reason")
			}
		})
	}
}

func TestStreamScopeCleanupCancelsAndPreservesExecutionOwnership(t *testing.T) {
	execution, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	scope, stop := streamContext(execution, time.Now().Add(time.Hour))
	stop()
	if scope.Err() != context.Canceled || execution.Err() != nil {
		t.Fatal("stream cleanup failed or cancelled the execution owner")
	}
}

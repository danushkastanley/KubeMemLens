package nodebinding

import (
	"context"
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracesession"
)

// The stream owns the same absolute deadline as its execution lease. Forwarding
// the lease timer through CancelCause races that deadline and changes Err() from
// DeadlineExceeded to Canceled, preventing the supervisor's bounded result drain.
// Other lease failures must still cancel the stream immediately.
func streamContext(execution context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	scope, stopDeadline := context.WithDeadlineCause(context.WithoutCancel(execution), deadline, tracesession.Stop(trace.Expired))
	scope, stopScope := context.WithCancelCause(scope)
	stopExecution := context.AfterFunc(execution, func() {
		cause := context.Cause(execution)
		if !errors.Is(cause, admission.ErrExpired) {
			stopScope(streamStop(cause))
		}
	})
	return scope, func() {
		stopExecution()
		stopScope(tracesession.Stop(trace.Cancelled))
		stopDeadline()
	}
}

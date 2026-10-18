package traceclient

import (
	"context"
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

type cancellation struct {
	done    chan struct{}
	cleanup Cleanup
	err     error
}

// Called with mu held. One compensating DELETE is shared by explicit cancel,
// transport failure and normal result teardown; it is never blindly retried.
func (s *Session) beginCancellationLocked() *cancellation {
	if s.cancellation != nil {
		return s.cancellation
	}
	work := &cancellation{done: make(chan struct{}), cleanup: CleanupUnconfirmed}
	s.cancellation = work
	a := s.admission
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
		defer cancel()
		cleanup, err := s.client.Cancel(ctx, a)
		s.mu.Lock()
		work.cleanup, work.err = cleanup, err
		s.value.Cleanup, s.value.CleanupFailure = cleanup, err
		stop := s.watchCancel
		s.notifyLocked()
		close(work.done)
		s.mu.Unlock()
		if stop != nil {
			if cleanup == CleanupConfirmed {
				time.AfterFunc(2*time.Second, stop)
			} else {
				stop()
			}
		}
	}()
	return work
}

func (s *Session) run(parent, createCtx context.Context, stopCreate context.CancelFunc) {
	defer stopCreate()
	stopParent := context.AfterFunc(parent, s.Cancel)
	defer stopParent()
	s.mu.Lock()
	a := s.admission
	s.mu.Unlock()
	var createErr error
	if a.id == "" {
		a, createErr = s.client.Create(createCtx, s.plan)
	}
	s.mu.Lock()
	s.operationCancel = nil
	s.admission = a
	s.value.AdmissionID = a.id
	if a.id == "" {
		if errors.Is(createErr, context.Canceled) || errors.Is(createErr, context.DeadlineExceeded) {
			s.value.Cleanup = CleanupNotRequested
			s.finishLocked(StateCancelled, createErr)
		} else {
			s.finishLocked(StateFailed, createErr)
		}
		s.mu.Unlock()
		return
	}
	if createErr != nil || s.cancelRequested {
		work := s.beginCancellationLocked()
		s.mu.Unlock()
		<-work.done
		s.mu.Lock()
		defer s.mu.Unlock()
		state := StateFailed
		if s.cancelRequested && work.cleanup == CleanupConfirmed && createErr == nil {
			state = StateCancelled
		}
		if createErr == nil && work.cleanup != CleanupConfirmed {
			createErr = work.err
		}
		s.finishLocked(state, createErr)
		return
	}
	// User cancellation is coordinated through DELETE, not an early stream
	// disconnect. Watch itself supplies duration/header/terminal deadlines.
	watchCtx, stopWatch := context.WithCancel(context.WithoutCancel(parent))
	defer stopWatch()
	s.watchCancel = stopWatch
	s.value.State = StateRunning
	s.notifyLocked()
	s.mu.Unlock()
	result, watchErr := s.client.Watch(watchCtx, a, func(value Result) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.value.Result = value
		s.notifyLocked()
	})
	s.mu.Lock()
	s.value.Result = result
	s.watchCancel = nil
	work := s.beginCancellationLocked()
	s.mu.Unlock()
	<-work.done
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelRequested {
		state, err := cancelledResult(result, watchErr, work.cleanup, work.err)
		s.finishLocked(state, err)
		return
	}
	state, err := terminalResult(result, watchErr)
	s.finishLocked(state, err)
}

func cancelledResult(result Result, streamErr error, cleanup Cleanup, cleanupErr error) (State, error) {
	// A valid observed terminal cause wins a race with a late cancel request.
	if streamErr == nil && result.TransportComplete {
		return terminalResult(result, nil)
	}
	var typed *Error
	// Cancellation may win before the activation endpoint writes its metadata.
	// A positive DELETE still establishes cancellation, but no terminal evidence
	// is invented for the unavailable/expired activation response.
	interrupted := errors.As(streamErr, &typed) && (typed.Kind == Incomplete || ((typed.Kind == Gone || typed.Kind == Unavailable) && result.Metadata.SessionID == ""))
	if streamErr != nil && !interrupted {
		return StateFailed, streamErr
	}
	if cleanup != CleanupConfirmed {
		return StateFailed, cleanupErr
	}
	return StateCancelled, streamErr
}

func terminalResult(result Result, err error) (State, error) {
	if err != nil {
		return StateFailed, err
	}
	summary, ok := result.Summary()
	if !ok || !result.TransportComplete {
		return StateFailed, failure(Incomplete)
	}
	switch summary.Termination {
	case trace.Expired:
		return StateCompleted, nil
	case trace.Cancelled:
		return StateCancelled, nil
	case trace.EventLimit, trace.OutputLimit:
		return StateTruncated, nil
	case trace.TargetChanged:
		return StateFailed, failure(TargetChanged)
	case trace.AuthorisationLost:
		return StateFailed, failure(Denied)
	default:
		return StateFailed, failure(Unavailable)
	}
}

package traceclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

type State string

const (
	StateNew        State = "new"
	StatePreflight  State = "preflight"
	StateReady      State = "ready"
	StateAdmitting  State = "admitting"
	StateAdmitted   State = "admitted"
	StateRunning    State = "running"
	StateCancelling State = "cancelling"
	StateCompleted  State = "completed"
	StateCancelled  State = "cancelled"
	StateTruncated  State = "truncated"
	StateFailed     State = "failed"
)

func (s State) Terminal() bool {
	return s == StateCompleted || s == StateCancelled || s == StateTruncated || s == StateFailed
}

const CleanupNotRequested Cleanup = "not_requested"

type Snapshot struct {
	State          State
	Selection      Selection
	Intent         Intent
	AdmissionID    string
	Result         Result
	Cleanup        Cleanup
	Failure        error
	CleanupFailure error
}

func (Snapshot) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private trace session]") }
func (Snapshot) MarshalJSON() ([]byte, error) { return nil, failure(Invalid) }

// Session owns one immutable selection and one admission attempt. Updates are
// wake-up signals, not an event queue; consumers read the latest Snapshot.
type Session struct {
	mu              sync.Mutex
	client          *Client
	plan            Plan
	admission       Admission
	value           Snapshot
	updates         chan struct{}
	done            chan struct{}
	operationCancel context.CancelFunc
	watchCancel     context.CancelFunc
	cancelRequested bool
	cancellation    *cancellation
}

func NewSession(c *Client, selection Selection, intent Intent) (*Session, error) {
	if c == nil {
		return nil, failure(Invalid)
	}
	if _, err := requestData(selection, intent); err != nil {
		return nil, err
	}
	return &Session{client: c, value: Snapshot{State: StateNew, Selection: selection, Intent: intent, Cleanup: CleanupNotRequested}, updates: make(chan struct{}, 1), done: make(chan struct{})}, nil
}

// NewAttachedSession observes an existing owned admission. It never selects a
// replacement target or creates another admission; stream metadata supplies the
// server-held intent. Start is still an explicit activation action.
func NewAttachedSession(c *Client, a Admission) (*Session, error) {
	if c == nil || a.client != c || a.id == "" {
		return nil, failure(Invalid)
	}
	return &Session{client: c, admission: a, value: Snapshot{State: StateAdmitted, AdmissionID: a.id, Cleanup: CleanupUnconfirmed}, updates: make(chan struct{}, 1), done: make(chan struct{})}, nil
}
func (s *Session) Updates() <-chan struct{} { return s.updates }
func (s *Session) Done() <-chan struct{}    { return s.done }
func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.value
	value.Failure = copyFailure(value.Failure)
	value.CleanupFailure = copyFailure(value.CleanupFailure)
	return value
}
func copyFailure(err error) error {
	var typed *Error
	if errors.As(err, &typed) {
		return failure(typed.Kind)
	}
	return err
}
func (s *Session) PreflightReport() (admission.PreflightDocument, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plan.PreflightReport(), s.plan.client != nil
}
func (s *Session) notifyLocked() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}
func (s *Session) finishLocked(state State, err error) {
	if s.value.State.Terminal() {
		return
	}
	s.value.State = state
	s.value.Failure = err
	s.operationCancel = nil
	s.watchCancel = nil
	s.notifyLocked()
	close(s.done)
}

// Prepare is non-activating and may be run by a CLI command or TUI command.
// Start is a separate explicit action after the operator reviews the result.
func (s *Session) Prepare(parent context.Context) error {
	s.mu.Lock()
	if s.value.State != StateNew {
		s.mu.Unlock()
		return failure(Invalid)
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	s.operationCancel = cancel
	s.value.State = StatePreflight
	s.notifyLocked()
	selection, intent := s.value.Selection, s.value.Intent
	s.mu.Unlock()
	plan, err := s.client.Preflight(ctx, selection, intent)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.operationCancel = nil
	if s.cancelRequested || parent.Err() != nil {
		s.finishLocked(StateCancelled, parent.Err())
		if err := ctx.Err(); err != nil {
			return err
		}
		return context.Canceled
	}
	if err != nil {
		s.finishLocked(StateFailed, err)
		return err
	}
	s.plan = plan
	s.value.State = StateReady
	s.notifyLocked()
	return nil
}

func (s *Session) Start(parent context.Context) error {
	s.mu.Lock()
	if s.value.State != StateReady && s.value.State != StateAdmitted {
		s.mu.Unlock()
		return failure(Invalid)
	}
	ctx, cancel := context.WithCancel(parent)
	s.operationCancel = cancel
	s.value.State = StateAdmitting
	s.value.Cleanup = CleanupUnconfirmed
	s.notifyLocked()
	s.mu.Unlock()
	go s.run(parent, ctx, cancel)
	return nil
}

// Cancel is idempotent and never blocks the UI. During streaming it requests
// confirmed server cancellation before interrupting the reader's bounded drain.
func (s *Session) Cancel() {
	s.mu.Lock()
	if s.value.State.Terminal() || s.cancelRequested {
		s.mu.Unlock()
		return
	}
	s.cancelRequested = true
	if s.value.State == StateAdmitted {
		s.value.State = StateCancelling
		work := s.beginCancellationLocked()
		s.notifyLocked()
		s.mu.Unlock()
		go func() {
			<-work.done
			s.mu.Lock()
			defer s.mu.Unlock()
			state := StateFailed
			if work.cleanup == CleanupConfirmed {
				state = StateCancelled
			}
			s.finishLocked(state, work.err)
		}()
		return
	}
	if s.value.State == StateNew || s.value.State == StateReady {
		s.finishLocked(StateCancelled, nil)
		s.mu.Unlock()
		return
	}
	s.value.State = StateCancelling
	s.notifyLocked()
	stop := s.operationCancel
	if s.admission.id != "" {
		s.beginCancellationLocked()
	}
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}

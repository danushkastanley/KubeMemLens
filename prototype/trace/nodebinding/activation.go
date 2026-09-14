package nodebinding

import (
	"context"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

// execution retains the cgroup reference until the runtime confirms teardown.
// Cancelling or expiring its context never closes that reference prematurely.
type execution struct {
	ctx           context.Context
	cancel        context.CancelCauseFunc
	stopDeadline  context.CancelFunc
	stopParent    func() bool
	done          chan struct{}
	once          sync.Once
	service       *Service
	id            string
	specification trace.Specification
	handle        targetfs.Handle
	cleanupErr    error
}

// activate uses only the intent frozen by bind. A stream cannot submit a new
// target, kind, disclosure policy or limits. The caller must establish programme
// approval before activation and call finish only after engine teardown.
func (s *Service) activate(parent context.Context, id string, deadline time.Time) (*execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.prune(now)
	if s.closed || s.cleanupErr != nil || parent.Err() != nil {
		return nil, admission.ErrUnavailable
	}
	l, ok := s.leases[id]
	if !ok {
		return nil, admission.ErrExpired
	}
	if l.execution != nil {
		return nil, admission.ErrExpired
	}
	if !deadline.After(now) || deadline.After(now.Add(l.specification.Bounds().Duration)) {
		return nil, admission.ErrTargetChanged
	}
	if err := l.handle.Check(parent); err != nil {
		_ = s.stopLease(id, l, admission.ErrTargetChanged)
		return nil, admission.ErrTargetChanged
	}
	lifetime, stopDeadline := context.WithDeadlineCause(s.ctx, deadline, admission.ErrExpired)
	lifetime, cancel := context.WithCancelCause(lifetime)
	e := &execution{ctx: lifetime, cancel: cancel, stopDeadline: stopDeadline, done: make(chan struct{}), service: s, id: id, specification: l.specification, handle: l.handle}
	e.stopParent = context.AfterFunc(parent, func() { cancel(context.Canceled); stopDeadline() })
	l.execution = e
	l.expires = deadline
	s.seen[id] = deadline
	return e, nil
}
func (e *execution) finish() error {
	e.once.Do(func() {
		e.stopParent()
		e.cancel(context.Canceled)
		e.stopDeadline()
		s := e.service
		s.mu.Lock()
		e.cleanupErr = s.closeHandle(e.handle)
		delete(s.leases, e.id)
		s.mu.Unlock()
		close(e.done)
	})
	return e.cleanupErr
}

// stopLease is called with mu held. Active work owns final release through
// finish; otherwise dropping the directory could allow identity reuse while
// the engine still holds attachments to the former cgroup.
func (s *Service) stopLease(id string, l *lease, cause error) error {
	if l.execution != nil {
		l.execution.cancel(cause)
		return nil
	}
	delete(s.leases, id)
	return s.closeHandle(l.handle)
}
func (s *Service) operation(ctx context.Context, id string, remove bool) error {
	s.mu.Lock()
	s.prune(time.Now())
	if s.closed || s.cleanupErr != nil || ctx.Err() != nil {
		s.mu.Unlock()
		return admission.ErrUnavailable
	}
	l, ok := s.leases[id]
	if !ok {
		s.mu.Unlock()
		if remove {
			return nil
		}
		return admission.ErrExpired
	}
	if !remove {
		err := l.handle.Check(ctx)
		if l.execution != nil && l.execution.ctx.Err() != nil {
			err = admission.ErrExpired
		}
		if err != nil {
			closeErr := s.stopLease(id, l, admission.ErrTargetChanged)
			s.mu.Unlock()
			if closeErr != nil {
				return admission.ErrUnavailable
			}
			return admission.ErrTargetChanged
		}
		s.mu.Unlock()
		return nil
	}
	err := s.stopLease(id, l, context.Canceled)
	active := l.execution
	s.mu.Unlock()
	if err != nil || active == nil {
		return err
	}
	select {
	case <-active.done:
		return active.cleanupErr
	case <-ctx.Done():
		return admission.ErrUnavailable
	}
}

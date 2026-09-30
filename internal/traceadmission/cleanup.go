package traceadmission

import (
	"context"
	"github.com/danushkastanley/kube-memlens/internal/traceaudit"
	"time"
)

func (m *Manager) finishAdmission(e *entry, err error) error {
	m.mu.Lock()
	e.initializing = false
	if err == nil && (m.closed || m.entries[e.id] != e || e.stage != admitted || !time.Now().Before(e.expires)) {
		err = ErrExpired
	}
	shouldClose := err != nil || e.stage == closing || m.closed
	m.mu.Unlock()
	if shouldClose {
		_ = m.discardWithCause(e.id, err)
	}
	return err
}

func (m *Manager) markClosing(e *entry, cause error) {
	if e.stage != closing {
		e.closeCause = cause
	}
	e.stage = closing
	m.wakeExpiry()
	if e.cancelActive != nil {
		e.cancelActive(cause)
	}
}
func (m *Manager) discard(id string) error { return m.discardWithCause(id, context.Canceled) }
func (m *Manager) discardWithCause(id string, cause error) error {
	return m.discardContext(context.Background(), id, cause)
}
func (m *Manager) discardContext(parent context.Context, id string, cause error) error {
	m.mu.Lock()
	e := m.entries[id]
	if e == nil {
		m.mu.Unlock()
		return nil
	}
	m.markClosing(e, cause)
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	return m.closeEntry(ctx, e)
}

// Records continue consuming every quota until the node confirms closure. A
// lost response or cleanup failure cannot create another free admission slot.
func (m *Manager) closeEntry(ctx context.Context, e *entry) error {
	m.mu.Lock()
	if m.entries[e.id] != e {
		m.mu.Unlock()
		return nil
	}
	if e.consumerRunning {
		done := e.consumerDone
		m.mu.Unlock()
		select {
		case <-done:
			return m.closeEntry(ctx, e)
		case <-ctx.Done():
			return ErrUnavailable
		}
	}
	if e.cleanupRunning {
		done := e.cleanupDone
		m.mu.Unlock()
		select {
		case <-done:
			m.mu.Lock()
			remaining := m.entries[e.id] == e
			m.mu.Unlock()
			if remaining {
				return ErrUnavailable
			}
			return nil
		case <-ctx.Done():
			return ErrUnavailable
		}
	}
	if e.initializing {
		m.mu.Unlock()
		return ErrUnavailable
	}
	e.cleanupRunning = true
	e.cleanupDone = make(chan struct{})
	binding := e.binding
	m.mu.Unlock()
	var err error
	if binding != nil {
		err = binding.Close(ctx)
	}
	m.mu.Lock()
	e.cleanupRunning = false
	close(e.cleanupDone)
	if err == nil {
		delete(m.entries, e.id)
	} else {
		e.retryCleanup = time.Now().Add(time.Second)
	}
	m.mu.Unlock()
	m.wakeExpiry()
	var terminalErr error
	// A rejected reservation never became a session. Its rejected create and
	// cleanup records are sufficient; do not invent an engine termination.
	if e.result.id != "" {
		terminalErr = m.recordTerminal(e.audit, terminationFromCause(e.closeCause), traceaudit.ControllerOutcome)
	}
	event := e.audit.event
	event.Operation, event.Decision, event.Reason = traceaudit.Cleanup, traceaudit.Observed, traceaudit.Cleaned
	if err != nil {
		event.Reason = traceaudit.CleanupUnconfirmed
	}
	auditErr := m.emitAudit(event)
	if err != nil || auditErr != nil || terminalErr != nil {
		return ErrUnavailable
	}
	return nil
}
func (m *Manager) sweep(shutdown bool) error {
	now := time.Now()
	m.mu.Lock()
	if shutdown {
		m.closed = true
	}
	ready := make([]*entry, 0, len(m.entries))
	for _, e := range m.entries {
		if shutdown || !now.Before(e.expires) {
			m.markClosing(e, ErrExpired)
		}
		if e.stage == closing && !e.initializing && !e.cleanupRunning && (shutdown || !now.Before(e.retryCleanup)) {
			ready = append(ready, e)
		}
	}
	m.mu.Unlock()
	if len(ready) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var err error
	for _, e := range ready {
		if m.closeEntry(ctx, e) != nil {
			err = ErrUnavailable
		}
	}
	return err
}
func (m *Manager) expiryLoop() {
	defer close(m.done)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		m.mu.Lock()
		next := m.nextExpiry(time.Now())
		m.mu.Unlock()
		timer.Stop()
		var ticks <-chan time.Time
		if !next.IsZero() {
			timer.Reset(time.Until(next))
			ticks = timer.C
		}
		select {
		case <-m.wake:
		case <-m.ctx.Done():
			m.mu.Lock()
			m.closed = true
			for _, e := range m.entries {
				m.markClosing(e, context.Canceled)
			}
			m.mu.Unlock()
			m.inflight.Wait()
			m.shutdownErr = m.sweep(true)
			m.mu.Lock()
			if len(m.entries) != 0 {
				m.shutdownErr = ErrUnavailable
			}
			m.mu.Unlock()
			return
		case <-ticks:
			_ = m.sweep(false)
		}
	}
}
func (m *Manager) Close(ctx context.Context) error {
	m.cancel()
	select {
	case <-m.done:
		return m.shutdownErr
	case <-ctx.Done():
		return ErrUnavailable
	}
}
func (m *Manager) discardOwned(owner [32]byte, namespace, id string) {
	m.mu.Lock()
	e := m.entries[id]
	owned := e != nil && e.owner == owner && e.request.namespace == namespace
	m.mu.Unlock()
	if owned {
		_ = m.discardWithCause(id, ErrDenied)
	}
}

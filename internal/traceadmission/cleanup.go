package traceadmission

import (
	"context"
	"time"
)

func (m *Manager) finishAdmission(e *entry, err error) {
	m.mu.Lock()
	e.initializing = false
	shouldClose := err != nil || e.stage == closing || m.closed
	m.mu.Unlock()
	if shouldClose {
		_ = m.discard(e.id)
	}
}

func (m *Manager) markClosing(e *entry, cause error) {
	e.stage = closing
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
	if err != nil {
		m.deps.Audit(AuditEvent{Operation: Cancel, Decision: "error", Reason: "cleanup_unconfirmed", Principal: "system"})
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
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
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
		case <-ticker.C:
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

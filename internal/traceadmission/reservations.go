package traceadmission

import (
	"context"
	"time"
)

func (m *Manager) reserve(owner [32]byte, r Request) (*entry, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	principal, namespace := 0, 0
	for _, e := range m.entries {
		if e.owner == owner {
			principal++
		}
		if e.request.namespace == r.namespace {
			namespace++
		}
	}
	if len(m.entries) >= m.policy.Global || principal >= m.policy.PerPrincipal || namespace >= m.policy.PerNamespace {
		return nil, ErrCapacity
	}
	if _, exists := m.entries[id]; exists {
		return nil, ErrUnavailable
	}
	e := &entry{id: id, owner: owner, request: r, expires: time.Now().Add(m.policy.PendingTTL)}
	m.entries[id] = e
	return e, nil
}

func (m *Manager) assignNode(e *entry, nodeUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.entries[e.id] != e || !time.Now().Before(e.expires) {
		return ErrExpired
	}
	count := 0
	for _, other := range m.entries {
		if other.nodeUID == nodeUID {
			count++
		}
	}
	if count >= m.policy.PerNode {
		return ErrCapacity
	}
	e.nodeUID = nodeUID
	return nil
}

func (m *Manager) discard(id string) error {
	m.mu.Lock()
	e := m.entries[id]
	delete(m.entries, id)
	m.mu.Unlock()
	if e != nil && e.binding != nil {
		return m.closeBindings([]Binding{e.binding})
	}
	return nil
}

// The single cleanup batch has one shared deadline, rather than a separate
// timeout for each item. Its work is bounded by the global reservation ceiling.
func (m *Manager) closeBindings(bindings []Binding) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var result error
	for _, b := range bindings {
		if err := b.Close(ctx); err != nil {
			result = ErrUnavailable
			m.deps.Audit(AuditEvent{Operation: Cancel, Decision: "error", Reason: "cleanup_unconfirmed", Principal: "system"})
		}
	}
	return result
}

func (m *Manager) expired(shutdown bool) []Binding {
	m.mu.Lock()
	defer m.mu.Unlock()
	if shutdown {
		m.closed = true
	}
	now := time.Now()
	bindings := []Binding{}
	for id, e := range m.entries {
		if !shutdown && now.Before(e.expires) {
			continue
		}
		delete(m.entries, id)
		if e.binding != nil {
			bindings = append(bindings, e.binding)
		}
	}
	return bindings
}

func (m *Manager) expiryLoop() {
	defer close(m.done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			bindings := m.expired(true)
			m.inflight.Wait()
			m.shutdownErr = m.closeBindings(bindings)
			return
		case <-ticker.C:
			m.closeBindings(m.expired(false))
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

// Authorisation loss can invalidate an existing owned reservation without
// revealing whether the supplied identifier belongs to another requester.
func (m *Manager) discardOwned(owner [32]byte, namespace, id string) {
	m.mu.Lock()
	e := m.entries[id]
	if e == nil || e.owner != owner || e.request.namespace != namespace {
		m.mu.Unlock()
		return
	}
	delete(m.entries, id)
	m.mu.Unlock()
	if e.binding != nil {
		m.closeBindings([]Binding{e.binding})
	}
}

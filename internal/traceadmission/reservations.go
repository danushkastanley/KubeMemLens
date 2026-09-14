package traceadmission

import (
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
	e := &entry{id: id, owner: owner, request: r, expires: time.Now().Add(m.policy.PendingTTL), initializing: true}
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

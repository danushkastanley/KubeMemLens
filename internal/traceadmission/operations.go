package traceadmission

import (
	"context"
	"k8s.io/apiserver/pkg/authentication/user"
	"time"
)

func (m *Manager) Get(ctx context.Context, info user.Info, namespace, id string) (result Admission, err error) {
	if !m.beginOperation() {
		return result, ErrUnavailable
	}
	defer m.inflight.Done()
	event := m.auditEvent(Read, Request{})
	defer func() {
		if m.record(event, err) != nil {
			result, err = Admission{}, ErrUnavailable
		}
	}()
	if m.AuditHealthy() != nil {
		return result, ErrUnavailable
	}
	principal, owner, err := snapshotPrincipal(info)
	if err != nil {
		return result, err
	}
	if err = m.identifyAudit(&event, principal, namespace, id); err != nil {
		return result, err
	}
	if !validNamespace(namespace) {
		return result, ErrInvalidRequest
	}
	ctx, stop := m.operationContext(ctx)
	defer stop()
	if err = m.deps.Authorizer.Trace(ctx, principal, Read, namespace, id); err != nil {
		m.discardOwned(owner, namespace, id)
		return result, safeDependencyError(err)
	}
	e, err := m.owned(owner, namespace, id)
	if err != nil {
		return result, err
	}
	auditEntry(&event, e)
	if err = m.deps.Authorizer.Pod(ctx, principal, e.request.namespace, e.request.pod); err != nil {
		m.discardWithCause(id, safeDependencyError(err))
		return result, safeDependencyError(err)
	}
	if err = m.deps.Resolver.Revalidate(ctx, e.workload); err != nil {
		m.discardWithCause(id, safeDependencyError(err))
		return result, safeDependencyError(err)
	}
	if err = e.binding.Revalidate(ctx); err != nil {
		m.discardWithCause(id, safeDependencyError(err))
		return result, safeDependencyError(err)
	}
	if err = m.access(ctx, principal, Read, e.request, id); err != nil {
		m.discardWithCause(id, err)
		return result, err
	}
	if ctx.Err() != nil {
		return result, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries[id] != e || (e.stage != admitted && e.stage != active) || !time.Now().Before(e.expires) {
		return result, ErrExpired
	}
	result = e.result
	if e.stage == active {
		result.state = ActiveState
		result.expiresAt = e.expires
	}
	return result, nil
}

func (m *Manager) Cancel(ctx context.Context, info user.Info, namespace, id string) (err error) {
	if !m.beginOperation() {
		return ErrUnavailable
	}
	defer m.inflight.Done()
	event := m.auditEvent(Cancel, Request{})
	defer func() {
		if m.record(event, err) != nil {
			err = ErrUnavailable
		}
	}()
	principal, owner, err := snapshotPrincipal(info)
	if err != nil {
		return err
	}
	if err = m.identifyAudit(&event, principal, namespace, id); err != nil {
		return err
	}
	if !validNamespace(namespace) {
		return ErrInvalidRequest
	}
	ctx, stop := m.operationContext(ctx)
	defer stop()
	if err = m.deps.Authorizer.Trace(ctx, principal, Cancel, namespace, id); err != nil {
		return safeDependencyError(err)
	}
	e, err := m.owned(owner, namespace, id)
	if err != nil {
		return err
	}
	auditEntry(&event, e)
	if err = m.deps.Authorizer.Pod(ctx, principal, e.request.namespace, e.request.pod); err != nil {
		return safeDependencyError(err)
	}
	return m.discard(id)
}

func (m *Manager) owned(owner [32]byte, namespace, id string) (*entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[id]
	// Missing, other-owner and other-namespace admissions have the same result.
	if e == nil || e.owner != owner || e.request.namespace != namespace || (e.stage != admitted && e.stage != active) {
		return nil, ErrNotFound
	}
	if m.closed || !time.Now().Before(e.expires) {
		return nil, ErrExpired
	}
	return e, nil
}

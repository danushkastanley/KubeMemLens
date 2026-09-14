package traceadmission

import (
	"context"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"k8s.io/apiserver/pkg/authentication/user"
	"time"
)

func (m *Manager) Get(ctx context.Context, info user.Info, namespace, id string) (result Admission, err error) {
	if !m.beginOperation() {
		return result, ErrUnavailable
	}
	defer m.inflight.Done()
	var kind trace.Kind
	principalClass := "unauthenticated"
	defer func() { m.record(Read, principalClass, kind, err) }()
	principal, owner, err := snapshotPrincipal(info)
	if err != nil {
		return result, err
	}
	if !validNamespace(namespace) {
		return result, ErrInvalidRequest
	}
	principalClass = principalCategory(principal)
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
	kind = e.request.kind
	if err = m.deps.Authorizer.Pod(ctx, principal, e.request.namespace, e.request.pod); err != nil {
		m.discard(id)
		return result, safeDependencyError(err)
	}
	if err = m.deps.Resolver.Revalidate(ctx, e.workload); err != nil {
		m.discard(id)
		return result, safeDependencyError(err)
	}
	if err = e.binding.Revalidate(ctx); err != nil {
		m.discard(id)
		return result, safeDependencyError(err)
	}
	if err = m.access(ctx, principal, Read, e.request, id); err != nil {
		m.discard(id)
		return result, err
	}
	if ctx.Err() != nil {
		return result, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries[id] != e || !time.Now().Before(e.expires) {
		return result, ErrExpired
	}
	return e.result, nil
}

func (m *Manager) Cancel(ctx context.Context, info user.Info, namespace, id string) (err error) {
	if !m.beginOperation() {
		return ErrUnavailable
	}
	defer m.inflight.Done()
	var kind trace.Kind
	principalClass := "unauthenticated"
	defer func() { m.record(Cancel, principalClass, kind, err) }()
	principal, owner, err := snapshotPrincipal(info)
	if err != nil {
		return err
	}
	if !validNamespace(namespace) {
		return ErrInvalidRequest
	}
	principalClass = principalCategory(principal)
	ctx, stop := m.operationContext(ctx)
	defer stop()
	if err = m.deps.Authorizer.Trace(ctx, principal, Cancel, namespace, id); err != nil {
		return safeDependencyError(err)
	}
	e, err := m.owned(owner, namespace, id)
	if err != nil {
		return err
	}
	kind = e.request.kind
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
	if e == nil || e.owner != owner || e.request.namespace != namespace || e.stage != admitted {
		return nil, ErrNotFound
	}
	if m.closed || !time.Now().Before(e.expires) {
		return nil, ErrExpired
	}
	return e, nil
}

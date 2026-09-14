package traceadmission

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"k8s.io/apiserver/pkg/authentication/user"
)

// Lease is a one-consumer claim, never a transport credential. Its deadline and
// specification cannot change. The node must separately activate its pending
// binding under this upper deadline before running an approved engine.
type Lease struct {
	manager    *Manager
	principal  *user.DefaultInfo
	admission  Admission
	binding    Binding
	workload   Workload
	namespace  string
	ctx        context.Context
	stopParent func() bool
}

func (l *Lease) Context() context.Context   { return l.ctx }
func (l *Lease) Admission() Admission       { return l.admission }
func (l *Lease) Binding() Binding           { return l.binding }
func (l *Lease) Deadline() time.Time        { deadline, _ := l.ctx.Deadline(); return deadline }
func (*Lease) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[active trace lease]") }
func (*Lease) MarshalJSON() ([]byte, error) { return nil, ErrInvalidRequest }

func (m *Manager) Claim(parent context.Context, info user.Info, namespace, id string) (lease *Lease, err error) {
	if !m.beginOperation() {
		return nil, ErrUnavailable
	}
	defer m.inflight.Done()
	principal, owner, err := snapshotPrincipal(info)
	if err != nil {
		return nil, err
	}
	defer func() { m.record(Attach, principalCategory(principal), "", err) }()
	if !validNamespace(namespace) {
		return nil, ErrInvalidRequest
	}
	ctx, stop := m.operationContext(parent)
	defer stop()
	if err = m.deps.Authorizer.Trace(ctx, principal, Attach, namespace, id); err != nil {
		return nil, safeDependencyError(err)
	}
	admission, err := m.Get(ctx, principal, namespace, id)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[id]
	if e == nil || e.owner != owner || e.request.namespace != namespace {
		return nil, ErrNotFound
	}
	if e.stage != admitted || e.initializing {
		return nil, ErrCapacity
	}
	if ctx.Err() != nil || !time.Now().Before(e.expires) {
		return nil, ErrExpired
	}
	expires := time.Now().Add(admission.specification.Bounds().Duration)
	lifetime, deadlineCancel := context.WithDeadlineCause(m.ctx, expires, ErrExpired)
	lifetime, cancel := context.WithCancelCause(lifetime)
	e.cancelActive = func(cause error) { cancel(cause); deadlineCancel() }
	e.expires = expires
	e.stage = active
	e.consumerRunning = true
	e.consumerDone = make(chan struct{})
	admission.state = ActiveState
	admission.expiresAt = expires
	stopParent := context.AfterFunc(parent, func() {
		cancel(context.Canceled)
		deadlineCancel()
		m.endConsumer(id)
		_ = m.discardWithCause(id, context.Canceled)
	})
	return &Lease{manager: m, principal: principal, admission: admission, binding: e.binding, workload: e.workload, namespace: namespace, ctx: lifetime, stopParent: stopParent}, nil
}
func (l *Lease) Revalidate(ctx context.Context) error {
	if l.ctx.Err() != nil {
		return safeLeaseCause(context.Cause(l.ctx))
	}
	bounded, stop := l.manager.operationContext(ctx)
	defer stop()
	if err := l.manager.deps.Authorizer.Trace(bounded, l.principal, Attach, l.namespace, l.admission.id); err != nil {
		_ = l.manager.discardWithCause(l.admission.id, safeDependencyError(err))
		return safeDependencyError(err)
	}
	_, err := l.manager.Get(bounded, l.principal, l.namespace, l.admission.id)
	return err
}
func (l *Lease) Close(ctx context.Context) error {
	l.stopParent()
	l.manager.endConsumer(l.admission.id)
	return l.manager.discardContext(ctx, l.admission.id, context.Canceled)
}
func safeLeaseCause(err error) error {
	if err == context.Canceled {
		return context.Canceled
	}
	return safeDependencyError(err)
}

// RevalidateStream uses the authenticated stream for node liveness, avoiding a
// race with a fast completed engine whose descriptor has already been released.
// Buffered frames may drain for at most two seconds after normal expiry, while
// current Kubernetes authority and immutable workload identity remain required.
func (l *Lease) RevalidateStream(ctx context.Context) error {
	if l.ctx.Err() != nil && !errors.Is(context.Cause(l.ctx), ErrExpired) {
		return safeLeaseCause(context.Cause(l.ctx))
	}
	if time.Now().After(l.Deadline().Add(2 * time.Second)) {
		return ErrExpired
	}
	bounded, stop := l.manager.operationContext(ctx)
	defer stop()
	m := l.manager
	if err := m.deps.Authorizer.Trace(bounded, l.principal, Attach, l.namespace, l.admission.id); err != nil {
		return safeDependencyError(err)
	}
	if err := m.deps.Authorizer.Pod(bounded, l.principal, l.namespace, l.workload.Target.PodName); err != nil {
		return safeDependencyError(err)
	}
	if err := m.deps.Resolver.Revalidate(bounded, l.workload); err != nil {
		return safeDependencyError(err)
	}
	if err := m.deps.Authorizer.Trace(bounded, l.principal, Read, l.namespace, l.admission.id); err != nil {
		return safeDependencyError(err)
	}
	return nil
}
func (m *Manager) endConsumer(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[id]; e != nil && e.consumerRunning {
		e.consumerRunning = false
		close(e.consumerDone)
	}
}

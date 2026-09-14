package traceadmission

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"k8s.io/apiserver/pkg/authentication/user"
)

type testAuthorizer struct {
	denied atomic.Bool
	calls  atomic.Int64
}

func (a *testAuthorizer) Trace(ctx context.Context, _ user.Info, _ Operation, namespace, _ string) error {
	a.calls.Add(1)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if a.denied.Load() || namespace == "forbidden" {
		return ErrDenied
	}
	return nil
}
func (a *testAuthorizer) Pod(ctx context.Context, _ user.Info, namespace, _ string) error {
	return a.Trace(ctx, nil, Read, namespace, "")
}

type testResolver struct {
	calls      atomic.Int64
	changed    atomic.Bool
	resolve    func(context.Context, Request) (Workload, error)
	revalidate func(context.Context, Workload) error
}

func (r *testResolver) Resolve(ctx context.Context, q Request) (Workload, error) {
	r.calls.Add(1)
	if r.resolve != nil {
		return r.resolve(ctx, q)
	}
	return testWorkload(q), nil
}
func (r *testResolver) Revalidate(ctx context.Context, w Workload) error {
	if r.revalidate != nil {
		return r.revalidate(ctx, w)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if r.changed.Load() {
		return ErrTargetChanged
	}
	return nil
}
func testWorkload(r Request) Workload {
	return Workload{Target: trace.TargetIdentity{Namespace: r.namespace, PodName: r.pod, PodUID: "uid-" + r.namespace, ContainerName: r.container, ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), NodeUID: "node-uid"}, NodeName: "node-one", QoS: "Burstable"}
}

type testBinding struct {
	target   trace.TargetIdentity
	profile  string
	closed   atomic.Bool
	once     sync.Once
	released chan struct{}
	closeErr error
}

func (b *testBinding) Target() trace.TargetIdentity { return b.target }
func (b *testBinding) ProfileDigest() string        { return b.profile }
func (b *testBinding) Revalidate(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if b.closed.Load() {
		return ErrTargetChanged
	}
	return nil
}
func (b *testBinding) Close(context.Context) error {
	if b.closeErr != nil {
		return b.closeErr
	}
	b.once.Do(func() { b.closed.Store(true); close(b.released) })
	return nil
}

type testBinder struct {
	calls    atomic.Int64
	mu       sync.Mutex
	bindings []*testBinding
	bind     func(context.Context, string, Workload, time.Time) (Binding, error)
}

func (b *testBinder) Bind(ctx context.Context, id string, w Workload, expires time.Time) (Binding, error) {
	b.calls.Add(1)
	if b.bind != nil {
		return b.bind(ctx, id, w, expires)
	}
	return b.makeBinding(w), nil
}
func (b *testBinder) makeBinding(w Workload) *testBinding {
	target := w.Target
	target.CgroupID = 42
	binding := &testBinding{target: target, profile: tracepreflight.Baseline().Digest(), released: make(chan struct{})}
	b.mu.Lock()
	b.bindings = append(b.bindings, binding)
	b.mu.Unlock()
	return binding
}
func (b *testBinder) first() *testBinding { b.mu.Lock(); defer b.mu.Unlock(); return b.bindings[0] }

type harness struct {
	manager  *Manager
	auth     *testAuthorizer
	resolver *testResolver
	binder   *testBinder
	mu       sync.Mutex
	audits   []AuditEvent
}

func newHarness(t *testing.T, policy Policy) *harness {
	t.Helper()
	h := &harness{auth: &testAuthorizer{}, resolver: &testResolver{}, binder: &testBinder{}}
	ctx, cancel := context.WithCancel(context.Background())
	manager, err := NewManager(ctx, Dependencies{Authorizer: h.auth, Resolver: h.resolver, Binder: h.binder, Audit: func(e AuditEvent) { h.mu.Lock(); defer h.mu.Unlock(); h.audits = append(h.audits, e) }}, policy)
	if err != nil {
		t.Fatal(err)
	}
	h.manager = manager
	t.Cleanup(func() {
		cancel()
		ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		if err := manager.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return h
}
func actor(name string) *user.DefaultInfo {
	return &user.DefaultInfo{Name: name, UID: "uid-" + name, Groups: []string{user.AllAuthenticated, "team"}, Extra: map[string][]string{"scope": {"private-claim"}}}
}
func requestFor(t *testing.T, namespace string) Request {
	t.Helper()
	r, err := DecodeRequest(namespace, strings.NewReader(validRequest))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (h *harness) auditText() string { h.mu.Lock(); defer h.mu.Unlock(); return fmt.Sprint(h.audits) }

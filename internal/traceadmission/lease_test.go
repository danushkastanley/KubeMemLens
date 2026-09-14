package traceadmission

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"k8s.io/apiserver/pkg/authentication/user"
)

func TestClaimHasOneOwnerAndOneConsumer(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	p := actor("owner")
	admitted, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.manager.Claim(context.Background(), actor("other"), "tenant-a", admitted.ID()); !errors.Is(err, ErrNotFound) {
		t.Fatal("other principal claimed stream")
	}
	var group sync.WaitGroup
	results := make(chan *Lease, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			lease, err := h.manager.Claim(context.Background(), p, "tenant-a", admitted.ID())
			results <- lease
			failures <- err
		}()
	}
	group.Wait()
	close(results)
	close(failures)
	var owned *Lease
	success := 0
	for lease := range results {
		if lease != nil {
			owned = lease
			success++
		}
	}
	if success != 1 {
		t.Fatal("claim was not atomic")
	}
	for err := range failures {
		if err != nil && !errors.Is(err, ErrCapacity) {
			t.Fatal(err)
		}
	}
	if err := owned.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err := h.manager.Get(context.Background(), p, "tenant-a", admitted.ID())
	if err != nil || current.State() != ActiveState || !current.ExpiresAt().Equal(owned.Deadline()) || current.Specification() != admitted.Specification() {
		t.Fatal("active status changed immutable intent or lost deadline")
	}
	if admitted.State() != AdmittedState {
		t.Fatal("claim mutated prior admission value")
	}

	if _, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a")); !errors.Is(err, ErrCapacity) {
		t.Fatal("active claim released quota")
	}
	if err := owned.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owned.Context().Err() == nil || !h.binder.first().closed.Load() {
		t.Fatal("claim cancellation did not release binding")
	}
	if _, err := h.manager.Claim(context.Background(), p, "tenant-a", admitted.ID()); !errors.Is(err, ErrNotFound) {
		t.Fatal("closed stream resumed")
	}
}

type streamPolicy struct {
	delegate *testAuthorizer
	deny     bool
	mu       sync.Mutex
}

func (a *streamPolicy) Trace(ctx context.Context, p user.Info, op Operation, ns, id string) error {
	a.mu.Lock()
	denied := a.deny && op == Attach
	a.mu.Unlock()
	if denied {
		return ErrDenied
	}
	return a.delegate.Trace(ctx, p, op, ns, id)
}
func (a *streamPolicy) Pod(ctx context.Context, p user.Info, ns, pod string) error {
	return a.delegate.Pod(ctx, p, ns, pod)
}
func TestStreamPermissionIsIndependentAndRevocationCancelsClaim(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	policy := &streamPolicy{delegate: h.auth, deny: true}
	h.manager.deps.Authorizer = policy
	p := actor("owner")
	admitted, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.manager.Claim(context.Background(), p, "tenant-a", admitted.ID()); !errors.Is(err, ErrDenied) {
		t.Fatal("get permission implied stream permission")
	}
	policy.mu.Lock()
	policy.deny = false
	policy.mu.Unlock()
	lease, err := h.manager.Claim(context.Background(), p, "tenant-a", admitted.ID())
	if err != nil {
		t.Fatal(err)
	}
	policy.mu.Lock()
	policy.deny = true
	policy.mu.Unlock()
	if err := lease.Revalidate(context.Background()); !errors.Is(err, ErrDenied) {
		t.Fatal("revocation did not fail closed")
	}
	if !errors.Is(context.Cause(lease.Context()), ErrDenied) {
		t.Fatal("revocation did not cancel active ownership")
	}
	if err := lease.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.binder.first().closed.Load() {
		t.Fatal("consumer teardown did not release binding")
	}
}
func TestLostBindResponseRetainsCleanupAuthorityAndQuota(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	h.binder.bind = func(_ context.Context, _ string, w Workload, _ Request, _ time.Time) (Binding, error) {
		b := h.binder.makeBinding(w)
		b.closeErr = errors.New("unconfirmed remote cleanup")
		return b, ErrUnavailable
	}
	p := actor("owner")
	if _, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a")); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a")); !errors.Is(err, ErrCapacity) {
		t.Fatal("lost response freed uncertain quota")
	}
	b := h.binder.first()
	b.closeMu.Lock()
	b.closeErr = nil
	b.closeMu.Unlock()
	if err := h.manager.sweep(true); err != nil {
		t.Fatal(err)
	}
	if !b.closed.Load() {
		t.Fatal("cleanup authority was lost")
	}
}

func TestDisconnectClosesClaimWithoutExplicitRelease(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	p := actor("owner")
	admitted, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	lease, err := h.manager.Claim(ctx, p, "tenant-a", admitted.ID())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-h.binder.first().released:
	case <-time.After(time.Second):
		t.Fatal("disconnect left binding alive")
	}
	if !errors.Is(context.Cause(lease.Context()), context.Canceled) {
		t.Fatal("disconnect cause lost")
	}
}

package traceadmission

import (
	"context"
	"errors"
	"fmt"
	"k8s.io/apiserver/pkg/authentication/user"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeniedAndUnauthenticatedRequestsNeverLookUpTargets(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	for _, p := range []user.Info{nil, &user.DefaultInfo{Name: user.Anonymous, Groups: []string{user.AllUnauthenticated}}, &user.DefaultInfo{Name: "forged", Groups: []string{"admin"}}} {
		if _, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a")); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("unauthenticated: %v", err)
		}
	}
	if _, err := h.manager.Admit(context.Background(), actor("user-a"), requestFor(t, "forbidden")); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if h.resolver.calls.Load() != 0 || h.binder.calls.Load() != 0 {
		t.Fatal("denial looked up or bound a target")
	}
}

func TestAdmissionBindsExactLifetimeAndOriginalRequester(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	p := actor("user-a")
	admitted, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if admitted.Specification().Target().CgroupID != 42 || admitted.EngineDigest() == "" || admitted.ID() == "" {
		t.Fatal("incomplete admission")
	}
	if _, err := h.manager.Get(context.Background(), actor("user-b"), "tenant-a", admitted.ID()); !errors.Is(err, ErrNotFound) {
		t.Fatal("another requester acquired admission")
	}
	if _, err := h.manager.Get(context.Background(), p, "tenant-b", admitted.ID()); !errors.Is(err, ErrNotFound) {
		t.Fatal("namespace retargeting accepted")
	}
	if _, err := h.manager.Get(context.Background(), p, "tenant-a", "unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown ID differs from other-owner result")
	}
	if _, err := h.manager.Get(context.Background(), p, "tenant-a", admitted.ID()); err != nil {
		t.Fatal(err)
	}
	if err := h.manager.Cancel(context.Background(), p, "tenant-a", admitted.ID()); err != nil {
		t.Fatal(err)
	}
	if !h.binder.first().closed.Load() {
		t.Fatal("cancellation retained node binding")
	}
}

func TestPodReplacementAndWarmRevocationInvalidateAdmissions(t *testing.T) {
	for _, condition := range []string{"replacement", "revocation"} {
		t.Run(condition, func(t *testing.T) {
			h := newHarness(t, DefaultPolicy())
			p := actor("user-a")
			admitted, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a"))
			if err != nil {
				t.Fatal(err)
			}
			if condition == "replacement" {
				h.resolver.changed.Store(true)
			} else {
				h.auth.denied.Store(true)
			}
			if _, err := h.manager.Get(context.Background(), p, "tenant-a", admitted.ID()); err == nil {
				t.Fatal("stale authorisation or target remained valid")
			}
			if !h.binder.first().closed.Load() {
				t.Fatal("replaced target retained binding")
			}
		})
	}
}

func TestRevocationDuringBindingReleasesQuotaAndHandle(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	h.resolver.revalidate = func(context.Context, Workload) error { h.auth.denied.Store(true); return nil }
	if _, err := h.manager.Admit(context.Background(), actor("user-a"), requestFor(t, "tenant-a")); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if !h.binder.first().closed.Load() {
		t.Fatal("revoked admission retained handle")
	}
	h.manager.mu.Lock()
	count := len(h.manager.entries)
	h.manager.mu.Unlock()
	if count != 0 {
		t.Fatal("revoked admission retained quota")
	}
}

func TestConcurrentNodeQuotaRejectsBeforeNodeWork(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := range 16 {
		r := requestFor(t, fmt.Sprintf("tenant-%d", i))
		p := actor(fmt.Sprintf("user-%d", i))
		wg.Go(func() {
			_, err := h.manager.Admit(context.Background(), p, r)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrCapacity) {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 1 || h.binder.calls.Load() != 1 {
		t.Fatalf("accepted=%d node calls=%d", accepted.Load(), h.binder.calls.Load())
	}
}

func TestPrincipalQuotaCountsPendingResolution(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	entered := make(chan struct{})
	release := make(chan struct{})
	h.resolver.resolve = func(ctx context.Context, r Request) (Workload, error) {
		close(entered)
		select {
		case <-release:
			return testWorkload(r), nil
		case <-ctx.Done():
			return Workload{}, ctx.Err()
		}
	}
	result := make(chan error, 1)
	p := actor("user-a")
	r := requestFor(t, "tenant-a")
	go func() { _, err := h.manager.Admit(context.Background(), p, r); result <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("resolution never started")
	}
	if _, err := h.manager.Admit(context.Background(), p, r); !errors.Is(err, ErrCapacity) {
		t.Fatal("pending request did not occupy quota")
	}
	if h.resolver.calls.Load() != 1 {
		t.Fatal("over-quota request resolved a target")
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestAuditAndErrorOutputsDoNotRetainProtectedValues(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	h.resolver.resolve = func(context.Context, Request) (Workload, error) {
		return Workload{}, errors.New("private backend credential")
	}
	_, err := h.manager.Admit(context.Background(), actor("private-user"), requestFor(t, "tenant-a"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	output := h.auditText() + err.Error()
	for _, secret := range []string{"private-user", "private-pod", "private-claim", "private backend credential", "tenant-a"} {
		if strings.Contains(output, secret) {
			t.Fatalf("retained %s", secret)
		}
	}
}

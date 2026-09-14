package traceadmission

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExpiredAdmissionsReleaseNodeHandlesAndQuota(t *testing.T) {
	policy := DefaultPolicy()
	policy.PendingTTL = 200 * time.Millisecond
	h := newHarness(t, policy)
	p := actor("user-a")
	r := requestFor(t, "tenant-a")
	a, err := h.manager.Admit(context.Background(), p, r)
	if err != nil {
		t.Fatal(err)
	}
	binding := h.binder.first()
	select {
	case <-binding.released:
	case <-time.After(2 * time.Second):
		t.Fatal("expiry did not release binding")
	}
	if _, err := h.manager.Get(context.Background(), p, "tenant-a", a.ID()); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired admission remained accessible")
	}
	if _, err := h.manager.Admit(context.Background(), p, r); err != nil {
		t.Fatalf("expiry retained quota: %v", err)
	}
}

func TestShutdownCancelsPendingResolutionBeforeReturning(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	entered := make(chan struct{})
	h.resolver.resolve = func(ctx context.Context, _ Request) (Workload, error) {
		close(entered)
		<-ctx.Done()
		return Workload{}, ctx.Err()
	}
	result := make(chan error, 1)
	r := requestFor(t, "tenant-a")
	go func() { _, err := h.manager.Admit(context.Background(), actor("user-a"), r); result <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("resolution did not start")
	}
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := h.manager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrUnavailable) {
		t.Fatal("pending operation survived shutdown")
	}
	if h.binder.calls.Load() != 0 {
		t.Fatal("node work started after shutdown")
	}
	if _, err := h.manager.Admit(context.Background(), actor("user-a"), r); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed manager admitted work")
	}
}

func TestCancelledRequestClosesALateBinding(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	entered := make(chan struct{})
	h.binder.bind = func(ctx context.Context, _ string, w Workload, _ time.Time) (Binding, error) {
		b := h.binder.makeBinding(w)
		close(entered)
		<-ctx.Done()
		return b, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	r := requestFor(t, "tenant-a")
	go func() { _, err := h.manager.Admit(ctx, actor("user-a"), r); result <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("binding did not start")
	}
	cancel()
	if err := <-result; !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if !h.binder.first().closed.Load() {
		t.Fatal("late binding survived cancellation")
	}
	h.manager.mu.Lock()
	count := len(h.manager.entries)
	h.manager.mu.Unlock()
	if count != 0 {
		t.Fatal("cancelled request retained quota")
	}
}

func TestCleanupFailureIsReportedWithoutBackendDetails(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	h.binder.bind = func(_ context.Context, _ string, w Workload, _ time.Time) (Binding, error) {
		b := h.binder.makeBinding(w)
		b.closeErr = errors.New("private backend detail")
		return b, nil
	}
	p := actor("user-a")
	a, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	err = h.manager.Cancel(context.Background(), p, "tenant-a", a.ID())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal("cleanup uncertainty reported as success")
	}
	if text := h.auditText(); !strings.Contains(text, "cleanup_unconfirmed") || strings.Contains(text, "private backend detail") {
		t.Fatal("cleanup error was hidden or disclosed backend details")
	}
}

func TestBindingSubstitutionAndVersionSkewAreRejected(t *testing.T) {
	for _, scenario := range []string{"pod", "container", "node", "cgroup", "profile"} {
		t.Run(scenario, func(t *testing.T) {
			h := newHarness(t, DefaultPolicy())
			h.binder.bind = func(_ context.Context, _ string, w Workload, _ time.Time) (Binding, error) {
				b := h.binder.makeBinding(w)
				switch scenario {
				case "pod":
					b.target.PodUID = "recreated"
				case "container":
					b.target.ContainerID = strings.Repeat("b", 64)
				case "node":
					b.target.NodeUID = "different"
				case "cgroup":
					b.target.CgroupID = 0
				case "profile":
					b.profile = "unreviewed"
				}
				return b, nil
			}
			if _, err := h.manager.Admit(context.Background(), actor("user-a"), requestFor(t, "tenant-a")); !errors.Is(err, ErrTargetChanged) {
				t.Fatal("substituted binding accepted")
			}
			if !h.binder.first().closed.Load() {
				t.Fatal("rejected binding leaked")
			}
		})
	}
}

func TestResolverCannotRetargetOrPreselectKernelIdentity(t *testing.T) {
	for _, scenario := range []string{"namespace", "cgroup"} {
		t.Run(scenario, func(t *testing.T) {
			h := newHarness(t, DefaultPolicy())
			h.resolver.resolve = func(_ context.Context, r Request) (Workload, error) {
				w := testWorkload(r)
				if scenario == "namespace" {
					w.Target.Namespace = "other-tenant"
				} else {
					w.Target.CgroupID = 42
				}
				return w, nil
			}
			if _, err := h.manager.Admit(context.Background(), actor("user-a"), requestFor(t, "tenant-a")); !errors.Is(err, ErrTargetChanged) {
				t.Fatal("invalid resolver result accepted")
			}
			if h.binder.calls.Load() != 0 {
				t.Fatal("invalid resolver result dispatched node work")
			}
		})
	}
}

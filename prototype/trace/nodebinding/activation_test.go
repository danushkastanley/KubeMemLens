package nodebinding

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func prepareExecution(t *testing.T, f *fixture, duration time.Duration) (*execution, *testHandle) {
	t.Helper()
	id := strings.Repeat("7", 32)
	if _, err := f.client.Bind(context.Background(), id, workload(), testIntent(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	handle := <-f.handles
	execution, err := f.service.activate(context.Background(), id, time.Now().Add(duration))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := execution.finish(); err != nil {
			t.Error(err)
		}
	})
	return execution, handle
}
func TestActiveExpiryRetainsReferenceUntilConfirmedTeardown(t *testing.T) {
	f := setup(t)
	execution, handle := prepareExecution(t, f, 40*time.Millisecond)
	<-execution.ctx.Done()
	f.service.mu.Lock()
	f.service.prune(time.Now())
	_, held := f.service.leases[execution.id]
	_, seen := f.service.seen[execution.id]
	f.service.mu.Unlock()
	select {
	case <-handle.closed:
		t.Fatal("expiry released cgroup before engine teardown")
	default:
	}
	if !held || !seen {
		t.Fatal("expiry lost quota or replay protection")
	}
	if _, err := f.service.activate(context.Background(), execution.id, time.Now().Add(time.Second)); !errors.Is(err, admission.ErrExpired) {
		t.Fatal("expired active request reactivated")
	}
	if err := execution.finish(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handle.closed:
	default:
		t.Fatal("confirmed teardown retained reference")
	}
}
func TestNodeCancellationWaitsForEngineTeardown(t *testing.T) {
	f := setup(t)
	execution, handle := prepareExecution(t, f, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := f.service.operation(ctx, execution.id, true); !errors.Is(err, admission.ErrUnavailable) {
		t.Fatal("unconfirmed cleanup reported as success")
	}
	if execution.ctx.Err() == nil {
		t.Fatal("cancel did not stop execution")
	}
	select {
	case <-handle.closed:
		t.Fatal("cancel closed reference before teardown")
	default:
	}
	if err := execution.finish(); err != nil {
		t.Fatal(err)
	}
	if err := f.service.operation(context.Background(), execution.id, true); err != nil {
		t.Fatal("confirmed close was not idempotent")
	}
}
func TestNodeActivationUsesFrozenIntentAndRejectsChangedReplay(t *testing.T) {
	f := setup(t)
	execution, _ := prepareExecution(t, f, time.Second)
	if execution.specification.Kind() != trace.Files || execution.specification.Paths() != trace.OmitPaths || execution.specification.Bounds() != testIntent().Bounds() {
		t.Fatal("activation lost immutable intent")
	}
	changed, err := admission.DecodeRequest("tenant-a", strings.NewReader(`{"schemaVersion":1,"pod":"target","container":"worker","kind":"cache","maxEvents":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.Bind(context.Background(), execution.id, workload(), changed, time.Now().Add(time.Second)); !errors.Is(err, admission.ErrExpired) {
		t.Fatal("changed intent replay accepted")
	}
	if execution.specification.Kind() != trace.Files {
		t.Fatal("active intent mutated")
	}
}
func TestNodeCannotExtendBeyondAdmittedDuration(t *testing.T) {
	f := setup(t)
	id := strings.Repeat("6", 32)
	intent, err := admission.DecodeRequest("tenant-a", strings.NewReader(`{"schemaVersion":1,"pod":"target","container":"worker","kind":"files","durationSeconds":1}`))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := f.client.Bind(context.Background(), id, workload(), intent, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.activate(context.Background(), id, time.Now().Add(2*time.Second)); !errors.Is(err, admission.ErrTargetChanged) {
		t.Fatal("activation raised duration ceiling")
	}
	if err := binding.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

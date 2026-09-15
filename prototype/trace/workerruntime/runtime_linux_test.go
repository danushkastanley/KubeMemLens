package workerruntime

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerprocess"
)

func TestLauncherPassesOnlyFixedVerifiedDescriptors(t *testing.T) {
	r := fixtureRuntime(t)
	exported := make(chan *os.File, 1)
	r.exportTarget = fixtureExporter(exported)
	spec, handle := fixtureSpec(t, "target")
	prepared, err := r.Prepare(context.Background(), spec, handle)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.EngineDigest != r.configuration.EngineDigest() || prepared.ProgrammeDigest != r.configuration.ProgrammeDigest() {
		t.Fatal("installation identity lost")
	}
	select {
	case <-exported:
		t.Fatal("target duplicated before execution")
	default:
	}
	output := &fixtureOutput{}
	result, err := prepared.Engine.Run(context.Background(), spec, output)
	if err != nil || output.events.Load() != 1 || result.Counts.Produced == nil || *result.Counts.Produced != 1 {
		t.Fatal("verified fixture execution failed")
	}
	file := <-exported
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("execution retained target duplicate")
	}
	if handle.closed.Load() {
		t.Fatal("runtime closed borrowed target")
	}
	if _, err := prepared.Engine.Run(context.Background(), spec, output); err == nil {
		t.Fatal("one-use adapter repeated")
	}
}

func TestProductionExporterRejectsForgedHandle(t *testing.T) {
	r := fixtureRuntime(t)
	spec, handle := fixtureSpec(t, "target")
	prepared, err := r.Prepare(context.Background(), spec, handle)
	if err != nil {
		t.Fatal(err)
	}
	output := &fixtureOutput{}
	if _, err := prepared.Engine.Run(context.Background(), spec, output); err == nil || output.events.Load() != 0 {
		t.Fatal("forged resolver handle launched worker")
	}
	if handle.closed.Load() {
		t.Fatal("failed launch closed node-owned target")
	}
	r.mu.Lock()
	running := r.running
	poisoned := r.poisoned
	r.mu.Unlock()
	if running != 0 || poisoned {
		t.Fatal("pre-launch rejection lost normal cleanup")
	}
}

func TestCloseCancelsAndJoinsActiveWorker(t *testing.T) {
	r := fixtureRuntime(t)
	exported := make(chan *os.File, 1)
	r.exportTarget = fixtureExporter(exported)
	spec, handle := fixtureSpec(t, "wait-for-close")
	prepared, err := r.Prepare(context.Background(), spec, handle)
	if err != nil {
		t.Fatal(err)
	}
	output := &fixtureOutput{received: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() { _, err := prepared.Engine.Run(context.Background(), spec, output); done <- err }()
	select {
	case <-output.received:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatal("runtime failed to join active worker")
	}
	if err := <-done; err == nil {
		t.Fatal("runtime shutdown did not cancel active execution")
	}
	if handle.closed.Load() {
		t.Fatal("runtime shutdown stole node handle")
	}
	if _, err := (<-exported).Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("shutdown retained child target descriptor")
	}
	if _, err := r.Prepare(context.Background(), spec, handle); err == nil {
		t.Fatal("closed runtime prepared more work")
	}
	if err := r.Close(ctx); err != nil {
		t.Fatal("runtime close was not idempotent")
	}
}

func TestCloseRejectsPreviouslyPreparedWork(t *testing.T) {
	r := fixtureRuntime(t)
	exported := make(chan *os.File, 1)
	r.exportTarget = fixtureExporter(exported)
	spec, handle := fixtureSpec(t, "target")
	prepared, err := r.Prepare(context.Background(), spec, handle)
	if err != nil {
		t.Fatal(err)
	}
	if r.Close(context.Background()) != nil {
		t.Fatal("idle runtime close failed")
	}
	if _, err := prepared.Engine.Run(context.Background(), spec, &fixtureOutput{}); err == nil {
		t.Fatal("prepared work bypassed runtime shutdown")
	}
	select {
	case <-exported:
		t.Fatal("closed runtime exported target")
	default:
	}
}

func TestRuntimeCeilingAndQuarantine(t *testing.T) {
	r := fixtureRuntime(t)
	if r.enter() != nil || r.enter() != nil || r.enter() == nil {
		t.Fatal("runtime worker ceiling not enforced")
	}
	r.leave()
	r.leave()
	spec, handle := fixtureSpec(t, "target")
	r.exportTarget = func(context.Context, targetfs.Handle) (*os.File, error) { panic(workerprocess.ErrCleanupUnconfirmed) }
	prepared, err := r.Prepare(context.Background(), spec, handle)
	if err != nil {
		t.Fatal(err)
	}
	var failure any
	func() {
		defer func() { failure = recover() }()
		_, _ = prepared.Engine.Run(context.Background(), spec, &fixtureOutput{})
	}()
	if failure != workerprocess.ErrCleanupUnconfirmed {
		t.Fatal("cleanup uncertainty was converted to an ordinary error")
	}
	if r.Close(context.Background()) == nil {
		t.Fatal("quarantined runtime claimed successful shutdown")
	}
	if handle.closed.Load() {
		t.Fatal("quarantine released node-owned target")
	}
}

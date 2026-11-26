package workerruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func assertIdleImageReleased(t *testing.T, r *Runtime) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running != 0 || r.owned.image != nil {
		t.Fatal("idle runtime retains a worker or executable image")
	}
}

func replaceExecutable(t *testing.T, r *Runtime) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "replacement")
	if err := os.WriteFile(path, []byte("unaccepted executable"), 0500); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, r.executable); err != nil {
		t.Fatal(err)
	}
}

func TestIdleExecutableReverifiedBeforeEachActivation(t *testing.T) {
	r := fixtureRuntime(t)
	assertIdleImageReleased(t, r)
	r.exportTarget = fixtureExporter(make(chan *os.File, 2))
	spec, handle := fixtureSpec(t, "target")
	prepared, err := r.Prepare(context.Background(), spec, handle)
	if err != nil {
		t.Fatal(err)
	}
	assertIdleImageReleased(t, r)
	if _, err := prepared.Engine.Run(context.Background(), spec, &fixtureOutput{}); err != nil {
		t.Fatal(err)
	}
	assertIdleImageReleased(t, r)
	replaceExecutable(t, r)
	prepared, err = r.Prepare(context.Background(), spec, handle)
	if err != nil {
		t.Fatal(err)
	}
	out := &fixtureOutput{}
	if _, err := prepared.Engine.Run(context.Background(), spec, out); err == nil || out.events.Load() != 0 {
		t.Fatal("changed installation executed after an idle interval")
	}
	assertIdleImageReleased(t, r)
}

func TestChangedExecutableRejectedBeforeFirstWorker(t *testing.T) {
	r := fixtureRuntime(t)
	r.exportTarget = fixtureExporter(make(chan *os.File, 1))
	replaceExecutable(t, r)
	spec, handle := fixtureSpec(t, "target")
	prepared, err := r.Prepare(context.Background(), spec, handle)
	if err != nil {
		t.Fatal(err)
	}
	out := &fixtureOutput{}
	if _, err := prepared.Engine.Run(context.Background(), spec, out); err == nil || out.events.Load() != 0 {
		t.Fatal("startup verification authorised subsequently changed bytes")
	}
	assertIdleImageReleased(t, r)
}

func TestConcurrentWorkersShareImageUntilLastExit(t *testing.T) {
	r := fixtureRuntime(t)
	r.exportTarget = fixtureExporter(make(chan *os.File, 2))
	start := func() (context.CancelFunc, <-chan error) {
		t.Helper()
		spec, handle := fixtureSpec(t, "wait-for-close")
		prepared, err := r.Prepare(context.Background(), spec, handle)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		out := &fixtureOutput{received: make(chan struct{}, 1)}
		done := make(chan error, 1)
		go func() { _, err := prepared.Engine.Run(ctx, spec, out); done <- err }()
		select {
		case <-out.received:
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not produce its fixture observation")
		}
		return cancel, done
	}
	stopFirst, first := start()
	r.mu.Lock()
	image := r.owned.image
	r.mu.Unlock()
	// The existing immutable image remains valid despite a path replacement.
	// The next idle-to-active transition must check the changed installation.
	replaceExecutable(t, r)
	stopSecond, second := start()
	r.mu.Lock()
	sameImage := r.owned.image == image && r.running == 2
	r.mu.Unlock()
	if !sameImage {
		t.Fatal("concurrent workers retained separate executable copies")
	}
	join := func(stop context.CancelFunc, done <-chan error) {
		t.Helper()
		stop()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("cancelled worker reported success")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("cancelled worker did not terminate")
		}
	}
	join(stopFirst, first)
	if _, err := image.Stat(); err != nil {
		t.Fatal("image released while another worker was active")
	}
	join(stopSecond, second)
	assertIdleImageReleased(t, r)
	if _, err := image.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("last worker exit retained executable descriptor")
	}
	if err := r.enter(context.Background()); err == nil {
		_ = r.leave()
		t.Fatal("new activation reused an obsolete accepted image")
	}
	assertIdleImageReleased(t, r)
}

func TestCancelledActivationDoesNotRetainImage(t *testing.T) {
	r := fixtureRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.enter(ctx); err == nil {
		_ = r.leave()
		t.Fatal("cancelled activation was accepted")
	}
	assertIdleImageReleased(t, r)
}

func TestUnconfirmedImageReleaseQuarantinesRuntime(t *testing.T) {
	r := fixtureRuntime(t)
	if err := r.enter(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Simulate an ownership violation, so the normal release cannot confirm
	// closing its descriptor. The runtime must not report clean teardown.
	if err := r.owned.image.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.leave(); err == nil {
		t.Fatal("unconfirmed image cleanup reported success")
	}
	if err := r.Close(context.Background()); err == nil {
		t.Fatal("image cleanup failure did not quarantine runtime")
	}
	if err := r.enter(context.Background()); err == nil {
		t.Fatal("quarantined runtime accepted another activation")
	}
}

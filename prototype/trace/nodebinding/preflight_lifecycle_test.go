package nodebinding

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

type waitingPreviewRuntime struct{ entered chan struct{} }

func (p waitingPreviewRuntime) Prepare(ctx context.Context, _ trace.Specification, _ targetfs.Handle) (Prepared, error) {
	close(p.entered)
	<-ctx.Done()
	return Prepared{}, ctx.Err()
}

type uncertainPreviewHandle struct {
	target trace.TargetIdentity
	closed atomic.Bool
}

func (h *uncertainPreviewHandle) Target() trace.TargetIdentity { return h.target }
func (*uncertainPreviewHandle) Check(context.Context) error    { return nil }
func (h *uncertainPreviewHandle) Close() error {
	h.closed.Store(true)
	return errors.New("unconfirmed close")
}

func TestPreflightCleanupUncertaintyPoisonsService(t *testing.T) {
	c := newCertificates(t)
	_, peer := c.issue(t)
	target := workload().Target
	target.CgroupID = 123
	h := &uncertainPreviewHandle{target: target}
	resolutions := 0
	s, err := NewService(context.Background(), "node-uid", "node", peer, func(context.Context, admission.Workload) (targetfs.Handle, error) { resolutions++; return h, nil }, func(context.Context) (string, error) { return tracepreflight.Baseline().Digest(), nil }, func(string) {}, &previewRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if !errors.Is(s.Close(ctx), admission.ErrUnavailable) {
			t.Error("shutdown hid cleanup uncertainty")
		}
	})
	if err := s.SetStartupReport(startupReport()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result, err := s.inspectTarget(context.Background(), requestFor("", workload(), testIntent(), time.Time{}))
		if !errors.Is(err, admission.ErrUnavailable) || result.EngineDigest != "" {
			t.Fatal("cleanup uncertainty returned readiness", err)
		}
	}
	if !h.closed.Load() || resolutions != 1 {
		t.Fatal("failed close was ignored or poisoned service reused")
	}
}

func TestSlowPreflightDoesNotBlockCancellationAndShutdownJoinsIt(t *testing.T) {
	entered := make(chan struct{})
	f := setupRuntime(t, waitingPreviewRuntime{entered})
	if err := f.service.SetStartupReport(startupReport()); err != nil {
		t.Fatal(err)
	}
	binding, err := f.client.Bind(context.Background(), strings.Repeat("a", 32), workload(), testIntent(), time.Now().Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	boundHandle := <-f.handles
	finished := make(chan error, 1)
	go func() { _, err := f.client.Preflight(context.Background(), workload(), testIntent()); finished <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("preflight did not enter preparation")
	}
	previewHandle := <-f.handles
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := f.client.Preflight(ctx, workload(), testIntent()); !errors.Is(err, admission.ErrCapacity) {
		t.Fatal("second preflight was not bounded", err)
	}
	if err := binding.Close(ctx); err != nil {
		t.Fatal("preflight blocked cancellation", err)
	}
	select {
	case <-boundHandle.closed:
	default:
		t.Fatal("cancelled binding retained its handle")
	}
	if err := f.service.Close(ctx); err != nil {
		t.Fatal("shutdown did not join temporary preflight", err)
	}
	select {
	case <-previewHandle.closed:
	default:
		t.Fatal("shutdown completed with an open preflight handle")
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("shutdown returned successful preflight")
		}
	case <-ctx.Done():
		t.Fatal("preflight did not terminate")
	}
}

package nodebinding

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

type previewRuntime struct{ runs atomic.Int64 }

func (p *previewRuntime) Run(context.Context, trace.Specification, trace.Output) (trace.Result, error) {
	p.runs.Add(1)
	return trace.Result{}, errors.New("preflight cannot run an engine")
}
func (p *previewRuntime) Prepare(context.Context, trace.Specification, targetfs.Handle) (Prepared, error) {
	e, err := trace.NewEngine(p)
	return Prepared{Engine: e, EngineDigest: tracepreflight.Baseline().EngineDigest, ProgrammeDigest: fixtureProgramme, StreamVersion: 2}, err
}
func startupReport() tracepreflight.Report {
	p := tracepreflight.Baseline()
	r := tracepreflight.Report{SchemaVersion: 1, Scope: p.Scope, ProfileDigest: p.Digest(), TraceApproval: "pending-custom-programme-freeze", CapturedAt: time.Now().UTC(), State: tracepreflight.Supported}
	for _, id := range p.Checks {
		r.Checks = append(r.Checks, tracepreflight.Check{ID: id, State: tracepreflight.Supported, Reason: tracepreflight.Available})
	}
	return r
}
func TestTLSPreflightClosesTargetWithoutStartingOrReservingTrace(t *testing.T) {
	runtime := &previewRuntime{}
	f := setupRuntime(t, runtime)
	report := startupReport()
	if err := f.service.SetStartupReport(report); err != nil {
		t.Fatal(err)
	}
	report.Checks[0].Value = "caller mutation"
	for range 3 {
		result, err := f.client.Preflight(context.Background(), workload(), testIntent())
		if err != nil || result.Validate(trace.Files) != nil || result.Baseline.Checks[0].Value != "" {
			t.Fatal("preflight failed or report aliased", err)
		}
		handle := <-f.handles
		select {
		case <-handle.closed:
		default:
			t.Fatal("preflight retained cgroup handle")
		}
	}
	f.service.mu.Lock()
	leases, seen := len(f.service.leases), len(f.service.seen)
	f.service.mu.Unlock()
	if leases != 0 || seen != 0 || runtime.runs.Load() != 0 {
		t.Fatal("preflight created incident state or ran an engine")
	}
	if err := f.service.SetStartupReport(startupReport()); err == nil {
		t.Fatal("startup observation was replaceable")
	}
}
func TestPreflightRequiresObservedStartupAndReleasesFailedResolution(t *testing.T) {
	f := setupRuntime(t, &previewRuntime{})
	if _, err := f.client.Preflight(context.Background(), workload(), testIntent()); !errors.Is(err, admission.ErrUnavailable) {
		t.Fatal(err)
	}
	if err := f.service.SetStartupReport(startupReport()); err != nil {
		t.Fatal(err)
	}
	original := f.service.resolve
	f.service.resolve = func(ctx context.Context, w admission.Workload) (targetfs.Handle, error) {
		h, _ := original(ctx, w)
		return h, admission.ErrTargetChanged
	}
	if _, err := f.client.Preflight(context.Background(), workload(), testIntent()); !errors.Is(err, admission.ErrTargetChanged) {
		t.Fatal(err)
	}
	handle := <-f.handles
	select {
	case <-handle.closed:
	default:
		t.Fatal("failed resolution leaked target")
	}
}

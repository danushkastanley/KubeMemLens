package sdk

import (
	"context"
	"errors"
	"math"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/logger"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/operators"
)

// Adapter is one execution in a supervised worker process. It owns the supplied
// descriptor after NewAdapter succeeds. No constructor or ordinary test loads BPF.
type Adapter struct {
	lifetime  context.Context
	programme *filecache.Programme
	spec      trace.Specification
	target    *os.File
	ready     func() error
	consumed  atomic.Bool
}

func NewAdapter(lifetime context.Context, p *filecache.Programme, spec trace.Specification, target *os.File, ready func() error) (*Adapter, error) {
	if lifetime == nil || lifetime.Err() != nil || p == nil || target == nil || ready == nil || spec.Validate() != nil || p.Manifest().Kind != spec.Kind() || p.Manifest().Architecture != runtime.GOARCH {
		return nil, ErrWorker
	}
	return &Adapter{lifetime: lifetime, programme: p, spec: spec, target: target, ready: ready}, nil
}

func (a *Adapter) Run(parent context.Context, spec trace.Specification, output trace.Output) (result trace.Result, err error) {
	if !a.consumed.CompareAndSwap(false, true) {
		return trace.Result{}, ErrWorker
	}
	defer func() {
		if a.target.Close() != nil {
			result.Counts = trace.Counts{}
			result.Termination = trace.EngineFailed
			err = ErrWorker
		}
	}()
	result = trace.Result{Version: trace.ContractVersion, Termination: trace.EngineFailed, Incomplete: true, Correlation: &trace.Correlation{State: "unavailable"}}
	if parent.Err() != nil || spec != a.spec || output == nil || validateProfile(spec) != nil || targetfs.VerifyWorkerDescriptor(parent, a.target, spec.Target()) != nil {
		return result, ErrWorker
	}
	deadline := time.Now().Add(spec.Bounds().Duration)
	if bound, ok := parent.Deadline(); ok && bound.Before(deadline) {
		deadline = bound
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	clock, err := alignDeadline(deadline)
	if err != nil || clock.Duration <= 0 || clock.MonotonicNS > math.MaxUint64-uint64(clock.Duration) {
		return result, ErrWorker
	}
	inventory, err := filecache.Inspect(spec.Kind(), a.programme.Object())
	if err != nil {
		return result, ErrWorker
	}
	owned := &resources{spec: spec, target: a.target, inventory: inventory}
	diagnostic := &diagnostics{}
	// External supervision enforces the same bound even if SDK/kernel work does
	// not honour cancellation. A cancelled preparation never activates control.
	startup := time.AfterFunc(preparationTimeout, cancel)
	defer startup.Stop()
	gctx, instance, err := prepare(ctx, a.programme, spec, clock.MonotonicNS+uint64(clock.Duration), logger.NewFromGenericLogger(diagnostic), owned.beforeAttach)
	if err != nil {
		return result, ErrWorker
	}
	defer gctx.Cancel()
	lifetime := &lifecycle{context: gctx, instance: instance, resources: owned}
	defer func() {
		// Stop is required even after partial Start, before releasing maps. The
		// generic SDK Run only calls Close on that path and is not used here.
		stopErr := lifetime.close()
		if stopErr != nil || diagnostic.failed.Load() {
			result.Counts = trace.Counts{}
			result.Termination = trace.EngineFailed
			err = ErrWorker
		}
	}()
	if pre, ok := instance.(operators.PreStart); ok {
		if pre.PreStart(gctx) != nil {
			return result, ErrWorker
		}
	}
	if instance.Start(gctx) != nil || ctx.Err() != nil || diagnostic.failed.Load() {
		return result, ErrWorker
	}
	decoder, err := filecache.NewDecoder(spec, clock)
	if err != nil || a.ready() != nil || ctx.Err() != nil {
		return result, ErrWorker
	}
	before, beforeErr := sampleCgroup(ctx, a.target, spec.Target())
	started, err := monotonicNS()
	if err != nil || owned.enable(ctx) != nil {
		return result, ErrWorker
	}
	startup.Stop()
	read, readErr := readEvents(ctx, owned, decoder, output, cancel)
	// Quiesce emission and detach before reading final cumulative counters.
	if lifetime.stop() != nil {
		return result, ErrWorker
	}
	stopped, clockErr := monotonicNS()
	if clockErr == nil {
		result.StartedAt, result.EndedAt = observationWindow(clock, started, stopped)
	}
	counts, countErr := finalCounts(owned, read)
	if countErr == nil {
		result.Counts = counts
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.Termination = trace.Expired
	case parent.Err() != nil:
		result.Termination = trace.Cancelled
	case readErr != nil:
		result.Termination = trace.EngineFailed
	default:
		result.Termination = trace.Cancelled
	}
	// Only normal expiry may extend sampling into the process-exit grace.
	// The independent signal lifetime is never detached from cancellation.
	if result.Termination == trace.Expired && readErr == nil && countErr == nil && a.lifetime.Err() == nil {
		sampleCtx, stop := terminalSampleContext(a.lifetime, deadline)
		after, afterErr := sampleCgroup(sampleCtx, a.target, spec.Target())
		stop()
		uncertainty, clockErr := alignmentUncertainty(clock)
		var known *time.Duration
		if clockErr == nil {
			known = &uncertainty
		} else {
			result.StartedAt, result.EndedAt = time.Time{}, time.Time{}
		}
		result.Correlation = sampledCorrelation(spec, filecache.ObservationWindow{Start: result.StartedAt, End: result.EndedAt, Uncertainty: known}, before, after, beforeErr, afterErr)
	} else if _, clockErr := alignmentUncertainty(clock); clockErr != nil {
		result.StartedAt, result.EndedAt = time.Time{}, time.Time{}
	}
	if a.lifetime.Err() != nil {
		result.Correlation = nil
	}
	if countErr != nil || readErr != nil {
		result.Counts = trace.Counts{}
		return result, ErrWorker
	}
	return result, nil
}

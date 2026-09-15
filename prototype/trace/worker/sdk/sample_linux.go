package sdk

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
)

// Sampling uses the independent signal lifetime. Normal observation expiry may
// drain within the existing process-exit grace; SIGTERM still cancels the read.
// The supervisor owns the hard bound even if a kernel read does not return.
func terminalSampleContext(lifetime context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(lifetime, deadline.Add(workeripc.NormalExitGrace))
}

func sampleCgroup(ctx context.Context, target *os.File, identity trace.TargetIdentity) (filecache.Sample, error) {
	return readSample(ctx, identity, func(ctx context.Context) ([]byte, error) {
		return targetfs.ReadWorkerMemoryStat(ctx, target, identity)
	})
}

func readSample(ctx context.Context, identity trace.TargetIdentity, read func(context.Context) ([]byte, error)) (filecache.Sample, error) {
	if ctx.Err() != nil {
		return filecache.Sample{}, ctx.Err()
	}
	started := time.Now().UTC()
	data, err := read(ctx)
	ended := time.Now().UTC()
	if err != nil {
		return filecache.Sample{}, err
	}
	if ctx.Err() != nil {
		return filecache.Sample{}, ctx.Err()
	}
	return filecache.NewSample(identity, started, ended, data)
}

func sampledCorrelation(spec trace.Specification, window filecache.ObservationWindow, before, after filecache.Sample, beforeErr, afterErr error) *trace.Correlation {
	if errors.Is(beforeErr, admission.ErrTargetChanged) || errors.Is(afterErr, admission.ErrTargetChanged) {
		return &trace.Correlation{State: "target_changed"}
	}
	if beforeErr != nil || afterErr != nil {
		return &trace.Correlation{State: "unavailable"}
	}
	value := filecache.Correlate(spec, window, before, after)
	return &value
}

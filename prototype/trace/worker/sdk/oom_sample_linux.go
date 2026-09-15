package sdk

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/oomtrace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

type evidenceSample struct {
	cache filecache.Sample
	oom   oomtrace.Sample
	err   error
}

func sampleEvidence(ctx context.Context, target *os.File, spec trace.Specification) evidenceSample {
	if spec.Kind() == trace.OOM {
		value, err := readOOMSample(ctx, spec.Target(), func(ctx context.Context) (oomtrace.RawSample, error) {
			return targetfs.ReadWorkerOOMSample(ctx, target, spec.Target())
		})
		return evidenceSample{oom: value, err: err}
	}
	value, err := sampleCgroup(ctx, target, spec.Target())
	return evidenceSample{cache: value, err: err}
}

func readOOMSample(ctx context.Context, identity trace.TargetIdentity, read func(context.Context) (oomtrace.RawSample, error)) (oomtrace.Sample, error) {
	if ctx.Err() != nil {
		return oomtrace.Sample{}, ctx.Err()
	}
	started := time.Now().UTC()
	data, err := read(ctx)
	ended := time.Now().UTC()
	if err != nil {
		return oomtrace.Sample{}, err
	}
	if ctx.Err() != nil {
		return oomtrace.Sample{}, ctx.Err()
	}
	return oomtrace.NewSample(identity, started, ended, data)
}

func correlateEvidence(spec trace.Specification, window filecache.ObservationWindow, before, after evidenceSample, result *trace.Result) {
	if spec.Kind() != trace.OOM {
		result.Correlation = sampledCorrelation(spec, window, before.cache, after.cache, before.err, after.err)
		return
	}
	state := ""
	if errors.Is(before.err, admission.ErrTargetChanged) || errors.Is(after.err, admission.ErrTargetChanged) {
		state = "target_changed"
	} else if before.err != nil || after.err != nil {
		state = "unavailable"
	}
	value := trace.OOMCorrelation{Window: trace.CorrelationWindow{State: state}}
	if state == "" {
		value = oomtrace.Correlate(spec, window, before.oom, after.oom)
	}
	result.OOMCorrelation = &value
}

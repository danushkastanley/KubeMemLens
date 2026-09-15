package sdk

import (
	"context"
	"errors"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/oomtrace"
)

func TestOOMSampleHonoursCancellationBeforeAndDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readOOMSample(ctx, sampleIdentity(), func(context.Context) (oomtrace.RawSample, error) {
		t.Fatal("read started after cancellation")
		return oomtrace.RawSample{}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled OOM sample accepted")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	_, err = readOOMSample(ctx, sampleIdentity(), func(context.Context) (oomtrace.RawSample, error) {
		cancel()
		return oomtrace.RawSample{Current: []byte("0\n")}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("in-flight cancelled OOM data retained")
	}
}

func TestOOMCorrelationFailureKeepsItsOwnKindAndMissingState(t *testing.T) {
	spec, err := trace.NewSpecification(trace.OOM, sampleIdentity(), trace.OmitPaths, trace.DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		err   error
		state string
	}{{admission.ErrTargetChanged, "target_changed"}, {admission.ErrUnavailable, "unavailable"}} {
		var result trace.Result
		correlateEvidence(spec, filecache.ObservationWindow{}, evidenceSample{}, evidenceSample{err: fixture.err}, &result)
		if result.Correlation != nil || result.OOMCorrelation == nil || result.OOMCorrelation.Window.State != fixture.state || result.OOMCorrelation.Current.Before != nil {
			t.Fatal("OOM failure fabricated or crossed evidence types")
		}
	}
}

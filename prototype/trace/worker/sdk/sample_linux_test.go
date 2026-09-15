package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
)

func sampleIdentity() trace.TargetIdentity {
	return trace.TargetIdentity{Namespace: "tenant", PodName: "pod", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(10, 0).UTC(), NodeUID: "node", CgroupID: 42}
}

func TestSignalCancellationStopsTerminalSampling(t *testing.T) {
	lifetime, signal := context.WithCancel(context.Background())
	deadline := time.Now()
	ctx, stop := terminalSampleContext(lifetime, deadline)
	defer stop()
	bound, ok := ctx.Deadline()
	if !ok || !bound.Equal(deadline.Add(workeripc.NormalExitGrace)) {
		t.Fatal("sample extended process-exit grace")
	}
	signal()
	called := false
	_, err := readSample(ctx, sampleIdentity(), func(context.Context) ([]byte, error) { called = true; return []byte("file 0\n"), nil })
	if err == nil || called {
		t.Fatal("sample started after signal cancellation")
	}
}

func TestSampleDiscardsReadCancelledInFlight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := readSample(ctx, sampleIdentity(), func(context.Context) ([]byte, error) { cancel(); return []byte("file 123\n"), nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("sample retained data after cancellation")
	}
}

func TestExpiredGraceCannotReadAndInvalidSampleDoesNotBecomeZero(t *testing.T) {
	ctx, stop := terminalSampleContext(context.Background(), time.Now().Add(-workeripc.NormalExitGrace-time.Nanosecond))
	defer stop()
	_, err := readSample(ctx, sampleIdentity(), func(context.Context) ([]byte, error) { t.Fatal("read after grace"); return nil, nil })
	if err == nil {
		t.Fatal("expired sample accepted")
	}
	for _, text := range []string{"", "file broken\n", strings.Repeat("x", 16385)} {
		_, err := readSample(context.Background(), sampleIdentity(), func(context.Context) ([]byte, error) { return []byte(text), nil })
		if err == nil {
			t.Fatal("invalid cgroup evidence accepted")
		}
	}
	spec, err := trace.NewSpecification(trace.Cache, sampleIdentity(), trace.OmitPaths, trace.DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		err   error
		state string
	}{{admission.ErrTargetChanged, "target_changed"}, {admission.ErrUnavailable, "unavailable"}} {
		got := sampledCorrelation(spec, filecache.ObservationWindow{}, filecache.Sample{}, filecache.Sample{}, nil, tc.err)
		if got.State != tc.state || got.File.Before != nil {
			t.Fatal("failed sample fabricated evidence")
		}
	}
}

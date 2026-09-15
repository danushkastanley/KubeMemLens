package tracesession

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func oomSession(t *testing.T, bounds trace.Bounds, adapter adapterFunc, sink Sink) *Session {
	t.Helper()
	target := trace.TargetIdentity{Namespace: "tenant", PodName: "pod", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(10, 0).UTC(), NodeUID: "node", CgroupID: 123}
	spec, err := trace.NewSpecification(trace.OOM, target, trace.OmitPaths, bounds)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := trace.NewEngine(adapter)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewVersion(traceframe.Metadata{SessionID: strings.Repeat("b", 32), EngineDigest: "sha256:" + strings.Repeat("c", 64), ProgrammeDigest: "sha256:" + strings.Repeat("d", 64), Specification: spec}, engine, sink, allow, traceframe.OOMVersion)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func oomObserver(count int) adapterFunc {
	return func(ctx context.Context, _ trace.Specification, out trace.Output) (trace.Result, error) {
		start := time.Now().UTC()
		produced, zero := uint64(0), uint64(0)
		var err error
		for range count {
			produced++
			err = out.OOMDecision(trace.OOMDecision{ObservedAt: time.Now().UTC(), Scope: trace.OOMScopeCgroup})
			if err != nil {
				break
			}
		}
		termination := trace.Termination(reason(err))
		if err == nil {
			<-ctx.Done()
			termination = trace.Expired
		}
		end := time.Now().UTC()
		if deadline, ok := ctx.Deadline(); ok && end.After(deadline) {
			end = deadline.UTC()
		}
		return trace.Result{Version: trace.ContractVersion, StartedAt: start, EndedAt: end, Termination: termination, Counts: trace.Counts{Produced: &produced, Sampled: &zero, Lost: &zero, Rejected: &zero}, Incomplete: true, OOMCorrelation: &trace.OOMCorrelation{Window: trace.CorrelationWindow{State: "unavailable"}}}, err
	}
}

func TestOOMVersionSessionAccountsForMissingContextAndLimits(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.Events = 2
	sink := &bufferSink{}
	result := oomSession(t, bounds, oomObserver(3), sink).Run(context.Background())
	if result.Err != nil || !result.TerminalDelivered || result.Summary.Termination != trace.EventLimit || result.Summary.WrittenEvents != 2 || result.Summary.Aggregates == nil || result.Summary.Aggregates.OOM.MissingProcessContext != 2 || result.Summary.OOMCorrelation == nil {
		t.Fatal("OOM ceiling or missing context was not preserved")
	}
	r := traceframe.NewReader(bytes.NewReader(sink.data.Bytes()))
	for range 4 {
		frame, err := r.Next()
		if err != nil || frame.Version() != traceframe.OOMVersion {
			t.Fatal("invalid OOM session stream")
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatal("OOM session did not end")
	}
}

func TestOOMPartialWriteCannotClaimDelivery(t *testing.T) {
	calls := 0
	sink := sinkFunc(func(_ context.Context, data []byte) (int, error) {
		calls++
		if calls == 2 {
			return 3, io.ErrUnexpectedEOF
		}
		return len(data), nil
	})
	result := oomSession(t, trace.DefaultBounds(), oomObserver(1), sink).Run(context.Background())
	if result.TerminalDelivered || result.Err == nil || result.Summary.Aggregates.Observations != 0 || result.Summary.WrittenEvents != 0 || calls != 2 {
		t.Fatal("partial OOM write counted as delivery")
	}
}

func TestOOMAuthorityLossDiscardsTerminalEvidence(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	adapter := func(_ context.Context, _ trace.Specification, out trace.Output) (trace.Result, error) {
		if out.OOMDecision(trace.OOMDecision{ObservedAt: time.Now().UTC(), Scope: trace.OOMScopeUnknown}) != nil {
			t.Fatal("OOM fixture failed")
		}
		cancel(Stop(trace.AuthorisationLost))
		return trace.Result{Version: trace.ContractVersion, Incomplete: true, OOMCorrelation: &trace.OOMCorrelation{Window: trace.CorrelationWindow{State: "unavailable"}}}, nil
	}
	result := oomSession(t, trace.DefaultBounds(), adapter, &bufferSink{}).Run(ctx)
	if !result.TerminalDelivered || result.Summary.Termination != trace.AuthorisationLost || result.Summary.Aggregates != nil || result.Summary.OOMCorrelation != nil || result.Summary.EngineCounts.Produced != nil {
		t.Fatal("authority loss retained OOM evidence")
	}
}

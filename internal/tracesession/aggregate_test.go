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

func aggregateSession(t *testing.T, kind trace.Kind, paths trace.PathPolicy, bounds trace.Bounds, adapter adapterFunc, sink Sink) *Session {
	t.Helper()
	target := trace.TargetIdentity{Namespace: "tenant", PodName: "pod", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(10, 0).UTC(), NodeUID: "node", CgroupID: 123}
	spec, err := trace.NewSpecification(kind, target, paths, bounds)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := trace.NewEngine(adapter)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewVersion(traceframe.Metadata{SessionID: strings.Repeat("b", 32), EngineDigest: "sha256:" + strings.Repeat("c", 64), ProgrammeDigest: "sha256:" + strings.Repeat("d", 64), Specification: spec}, engine, sink, allow, traceframe.AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func aggregateObserver(count int) adapterFunc {
	return func(ctx context.Context, spec trace.Specification, out trace.Output) (trace.Result, error) {
		start := time.Now().UTC()
		produced, zero, requested, completed := uint64(0), uint64(0), uint64(10), uint64(8)
		var err error
		for range count {
			produced++
			if spec.Kind() == trace.Files {
				path, _ := trace.NewSensitiveText("/fixture-only", 256)
				err = out.FileActivity(trace.FileActivity{ObservedAt: time.Now().UTC(), Operation: trace.FileRead, RequestedBytes: &requested, CompletedBytes: &completed, Path: path})
			} else {
				err = out.CacheActivity(trace.CacheActivity{ObservedAt: time.Now().UTC(), Operation: trace.CacheAdd, Pages: 2})
			}
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
		return trace.Result{Version: trace.ContractVersion, StartedAt: start, EndedAt: end, Termination: termination, Counts: trace.Counts{Produced: &produced, Sampled: &zero, Lost: &zero, Rejected: &zero}, Incomplete: true}, err
	}
}

func TestV2DefaultOutputContainsOnlyMetadataAndAggregates(t *testing.T) {
	for _, kind := range []trace.Kind{trace.Files, trace.Cache} {
		t.Run(string(kind), func(t *testing.T) {
			bounds := trace.DefaultBounds()
			bounds.Duration = 30 * time.Millisecond
			sink := &bufferSink{}
			result := aggregateSession(t, kind, trace.OmitPaths, bounds, aggregateObserver(2), sink).Run(context.Background())
			if result.Err != nil || !result.TerminalDelivered || result.Summary.Aggregates == nil || result.Summary.Aggregates.Observations != 2 || result.Summary.WrittenEvents != 0 || len(sink.frames) != 2 {
				t.Fatal("default stream was not aggregate-only")
			}
			if strings.Contains(sink.data.String(), "fixture-only") {
				t.Fatal("default output exposed a path")
			}
			reader := traceframe.NewReader(bytes.NewReader(sink.data.Bytes()))
			for range 2 {
				frame, err := reader.Next()
				if err != nil || frame.Version() != traceframe.AggregateVersion {
					t.Fatal("invalid v2 stream")
				}
			}
			if _, err := reader.Next(); err != io.EOF {
				t.Fatal("stream did not end")
			}
		})
	}
}

func TestV2ConfirmedPathsKeepEventsAndAggregateCountsAligned(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.Duration = 30 * time.Millisecond
	sink := &bufferSink{}
	result := aggregateSession(t, trace.Files, trace.ConfirmedPaths, bounds, aggregateObserver(2), sink).Run(context.Background())
	if result.Err != nil || !result.TerminalDelivered || result.Summary.WrittenEvents != 2 || result.Summary.Aggregates.Observations != 2 || *result.Summary.Aggregates.Reads.CompletedBytes.Value != 16 || len(sink.frames) != 4 {
		t.Fatal("confirmed stream lost events or totals")
	}
	reader := traceframe.NewReader(bytes.NewReader(sink.data.Bytes()))
	for range 4 {
		if _, err := reader.Next(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestV2CeilingCountsObservationsWithoutRawFrames(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.Events = 2
	sink := &bufferSink{}
	result := aggregateSession(t, trace.Cache, trace.OmitPaths, bounds, aggregateObserver(100), sink).Run(context.Background())
	if !result.TerminalDelivered || result.Summary.Termination != trace.EventLimit || result.Summary.Aggregates.Observations != 2 || result.Summary.WrittenEvents != 0 || len(sink.frames) != 2 {
		t.Fatal("aggregate observation ceiling failed")
	}
}

func TestV2AuthorisationLossClearsRichSummary(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	adapter := func(_ context.Context, _ trace.Specification, out trace.Output) (trace.Result, error) {
		if out.CacheActivity(trace.CacheActivity{ObservedAt: time.Now().UTC(), Operation: trace.CacheAdd, Pages: 2}) != nil {
			t.Fatal("fixture observation failed")
		}
		cancel(Stop(trace.AuthorisationLost))
		return trace.Result{Version: trace.ContractVersion, Incomplete: true}, nil
	}
	sink := &bufferSink{}
	result := aggregateSession(t, trace.Cache, trace.OmitPaths, trace.DefaultBounds(), adapter, sink).Run(ctx)
	if !result.TerminalDelivered || result.Summary.Termination != trace.AuthorisationLost || result.Summary.Aggregates != nil || result.Summary.EngineCounts.Produced != nil {
		t.Fatal("authorisation loss retained rich output")
	}
}

func TestV2PartialRawWriteCannotClaimAggregateDelivery(t *testing.T) {
	calls := 0
	sink := sinkFunc(func(_ context.Context, data []byte) (int, error) {
		calls++
		if calls == 2 {
			return 3, io.ErrUnexpectedEOF
		}
		return len(data), nil
	})
	result := aggregateSession(t, trace.Files, trace.ConfirmedPaths, trace.DefaultBounds(), aggregateObserver(2), sink).Run(context.Background())
	if result.TerminalDelivered || result.Err == nil || result.Summary.Aggregates.Observations != 0 || result.Summary.WrittenEvents != 0 || calls != 2 {
		t.Fatal("partial event write counted as aggregate delivery")
	}
}

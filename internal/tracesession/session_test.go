package tracesession

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

type adapterFunc func(context.Context, trace.Specification, trace.Output) (trace.Result, error)

func (f adapterFunc) Run(ctx context.Context, s trace.Specification, o trace.Output) (trace.Result, error) {
	return f(ctx, s, o)
}

type sinkFunc func(context.Context, []byte) (int, error)

func (f sinkFunc) WriteFrame(ctx context.Context, data []byte) (int, error) { return f(ctx, data) }

type bufferSink struct {
	mu     sync.Mutex
	data   bytes.Buffer
	frames []traceframe.Type
}

func (s *bufferSink) WriteFrame(ctx context.Context, data []byte) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	frame, err := traceframe.Decode(data)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, frame.Type())
	return s.data.Write(data)
}
func makeSession(t *testing.T, bounds trace.Bounds, adapter adapterFunc, sink Sink, validate Validate) *Session {
	t.Helper()
	target := trace.TargetIdentity{Namespace: "tenant", PodName: "pod", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(10, 0).UTC(), NodeUID: "node", CgroupID: 123}
	spec, err := trace.NewSpecification(trace.Files, target, trace.OmitPaths, bounds)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := trace.NewEngine(adapter)
	if err != nil {
		t.Fatal(err)
	}
	session, err := New(traceframe.Metadata{SessionID: strings.Repeat("b", 32), EngineDigest: "sha256:" + strings.Repeat("c", 64), ProgrammeDigest: "sha256:" + strings.Repeat("d", 64), Specification: spec}, engine, sink, validate)
	if err != nil {
		t.Fatal(err)
	}
	return session
}
func allow(context.Context) error { return nil }
func observing(count int) adapterFunc {
	return func(ctx context.Context, _ trace.Specification, out trace.Output) (trace.Result, error) {
		started := time.Now().UTC()
		produced, zero := uint64(0), uint64(0)
		for i := 0; i < count; i++ {
			produced++
			if err := out.FileActivity(trace.FileActivity{ObservedAt: time.Now().UTC(), Operation: trace.FileRead}); err != nil {
				return trace.Result{Version: 1, StartedAt: started, EndedAt: time.Now().UTC(), Counts: trace.Counts{Produced: &produced, Sampled: &zero, Lost: &zero, Rejected: &zero}}, err
			}
		}
		<-ctx.Done()
		return trace.Result{Version: 1, StartedAt: started, EndedAt: time.Now().UTC(), Termination: trace.Expired, Counts: trace.Counts{Produced: &produced, Sampled: &zero, Lost: &zero, Rejected: &zero}}, ctx.Err()
	}
}
func TestDurationProducesMetadataEventsAndOneSummary(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.Duration = 30 * time.Millisecond
	sink := &bufferSink{}
	session := makeSession(t, bounds, observing(2), sink, allow)
	outcome := session.Run(context.Background())
	if outcome.Err != nil || !outcome.TerminalDelivered || outcome.Summary.Termination != trace.Expired || outcome.Summary.Incomplete {
		t.Fatalf("bounded duration outcome: %+v", outcome)
	}
	if outcome.Summary.WrittenEvents != 2 || outcome.WrittenBytes != uint64(sink.data.Len()) {
		t.Fatal("incorrect accounting")
	}
	if len(sink.frames) != 4 || sink.frames[0] != traceframe.MetadataFrame || sink.frames[3] != traceframe.SummaryFrame {
		t.Fatal("incorrect frame order")
	}
	if second := session.Run(context.Background()); !errors.Is(second.Err, ErrConsumed) {
		t.Fatal("session replay accepted")
	}
}
func TestEventCeilingStopsEngineAndRetainsSummary(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.Events = 2
	sink := &bufferSink{}
	session := makeSession(t, bounds, observing(1000), sink, allow)
	result := session.Run(context.Background())
	if !result.TerminalDelivered || result.Summary.Termination != trace.EventLimit || result.Summary.WrittenEvents != 2 || !result.Summary.Incomplete {
		t.Fatalf("event limit: %+v", result)
	}
	if len(sink.frames) != 4 {
		t.Fatal("engine emitted past event ceiling")
	}
}
func TestPartialEventWriteDoesNotAppendSummaryToBrokenFrame(t *testing.T) {
	var calls int
	var actual int
	sink := sinkFunc(func(_ context.Context, data []byte) (int, error) {
		calls++
		if calls == 2 {
			actual += 3
			return 3, io.ErrUnexpectedEOF
		}
		actual += len(data)
		return len(data), nil
	})
	session := makeSession(t, trace.DefaultBounds(), observing(10), sink, allow)
	result := session.Run(context.Background())
	if result.TerminalDelivered || result.Err == nil || calls != 2 || result.WrittenBytes != uint64(actual) {
		t.Fatalf("partial write hidden: %+v calls=%d", result, calls)
	}
}
func TestInsufficientMetadataAndTerminalBudgetRejectsBeforeEngine(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.OutputBytes = traceframe.TerminalReserve
	called := false
	sink := &bufferSink{}
	session := makeSession(t, bounds, func(context.Context, trace.Specification, trace.Output) (trace.Result, error) {
		called = true
		return trace.Result{}, nil
	}, sink, allow)
	result := session.Run(context.Background())
	if result.Err != Stop(trace.OutputLimit) || called || sink.data.Len() != 0 {
		t.Fatal("unrepresentable stream began")
	}
}
func TestSinkDeadlineAndCurrentAuthorisationAreEnforced(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.Duration = 40 * time.Millisecond
	var calls atomic.Int32
	sink := sinkFunc(func(ctx context.Context, data []byte) (int, error) {
		if calls.Add(1) == 1 {
			return len(data), nil
		}
		<-ctx.Done()
		return 0, ctx.Err()
	})
	result := makeSession(t, bounds, observing(1), sink, allow).Run(context.Background())
	if result.TerminalDelivered || result.Err == nil || calls.Load() != 2 {
		t.Fatal("slow sink did not stop")
	}
	deniedSink := &bufferSink{}
	called := false
	session := makeSession(t, bounds, func(context.Context, trace.Specification, trace.Output) (trace.Result, error) {
		called = true
		return trace.Result{}, nil
	}, deniedSink, func(context.Context) error { return Stop(trace.AuthorisationLost) })
	result = session.Run(context.Background())
	if called || deniedSink.data.Len() != 0 || result.Err != Stop(trace.AuthorisationLost) {
		t.Fatal("authorisation failure began trace")
	}
}
func TestOutputCeilingReservesTerminalBytes(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.OutputBytes = 3500
	sink := &bufferSink{}
	result := makeSession(t, bounds, observing(1000), sink, allow).Run(context.Background())
	if result.Summary.Termination != trace.OutputLimit || !result.TerminalDelivered || result.WrittenBytes > bounds.OutputBytes {
		t.Fatalf("output budget: %+v", result)
	}
	if sink.frames[len(sink.frames)-1] != traceframe.SummaryFrame {
		t.Fatal("terminal reservation missing")
	}
}

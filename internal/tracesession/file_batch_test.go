package tracesession

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

type batchSink struct {
	data    bytes.Buffer
	writes  int
	maximum int
	fail    bool
	cancel  context.CancelCauseFunc
}

func (s *batchSink) WriteFrame(ctx context.Context, data []byte) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	s.writes++
	s.maximum = max(s.maximum, len(data))
	if s.fail {
		n, _ := s.data.Write(data[:3])
		return n, io.ErrUnexpectedEOF
	}
	n, err := s.data.Write(data)
	if s.cancel != nil {
		s.cancel(Stop(trace.AuthorisationLost))
	}
	return n, err
}

func batchOutput(t *testing.T, bounds trace.Bounds, paths trace.PathPolicy, sink Sink) *output {
	t.Helper()
	session := aggregateSession(t, trace.Files, paths, bounds, aggregateObserver(0), sink)
	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(nil) })
	start := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	return &output{ctx: ctx, cancel: cancel, sink: sink, spec: session.metadata.Specification, aggregates: session.aggregates, started: start, deadline: start.Add(bounds.Duration)}
}
func batchEvents(o *output, path string) []trace.FileActivity {
	text, _ := trace.NewSensitiveText(path, o.spec.Bounds().PathBytes)
	requested, completed := uint64(4096), uint64(128)
	events := make([]trace.FileActivity, trace.MaxFileBatch)
	for i := range events {
		events[i] = trace.FileActivity{ObservedAt: o.started.Add(time.Duration(i) * time.Millisecond), Operation: trace.FileRead, RequestedBytes: &requested, CompletedBytes: &completed, Path: text}
	}
	return events
}

func TestFileBatchPreservesBytesTotalsAndBoundedWrites(t *testing.T) {
	for _, path := range []string{"/fixture", strings.Repeat("\x1b", 256)} {
		t.Run("path-length-"+time.Duration(len(path)).String(), func(t *testing.T) {
			individual, batched := &batchSink{}, &batchSink{}
			a := batchOutput(t, trace.DefaultBounds(), trace.ConfirmedPaths, individual)
			b := batchOutput(t, trace.DefaultBounds(), trace.ConfirmedPaths, batched)
			events := batchEvents(a, path)
			for _, event := range events {
				if err := a.FileActivity(event); err != nil {
					t.Fatal(err)
				}
			}
			if err := b.FileActivities(events); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(individual.data.Bytes(), batched.data.Bytes()) || a.bytes != b.bytes || a.events != b.events || !reflect.DeepEqual(a.aggregates.Snapshot(), b.aggregates.Snapshot()) {
				t.Fatal("batch changed ordered frame bytes, accounting or aggregates")
			}
			if batched.writes >= individual.writes || batched.maximum > traceframe.MaxBytes {
				t.Fatal("batch did not coalesce within the transport bound")
			}
		})
	}
}

func TestFileBatchKeepsValidPrefixAtLimitsAndInvalidObservation(t *testing.T) {
	for _, scenario := range []string{"events", "bytes", "operation", "timestamp"} {
		t.Run(scenario, func(t *testing.T) {
			bounds := trace.DefaultBounds()
			if scenario == "events" {
				bounds.Events = 3
			}
			sink := &batchSink{}
			o := batchOutput(t, bounds, trace.ConfirmedPaths, sink)
			events := batchEvents(o, "/fixture")
			want := trace.EngineFailed
			switch scenario {
			case "events":
				want = trace.EventLimit
			case "bytes":
				want = trace.OutputLimit
				var prefix uint64
				for _, event := range events[:3] {
					frame, _ := traceframe.NewFileVersion(event, o.spec, traceframe.AggregateVersion)
					data, _ := traceframe.Encode(frame)
					prefix += uint64(len(data))
				}
				o.bytes = bounds.OutputBytes - traceframe.AggregateTerminalReserve - prefix
			case "operation":
				events[3].Operation = "invalid"
			case "timestamp":
				events[3].ObservedAt = o.deadline.Add(time.Second)
			}
			if err := o.FileActivities(events); err != Stop(want) {
				t.Fatalf("got %v, want %v", err, want)
			}
			if o.events != 3 || o.aggregates.Observations() != 3 || sink.writes != 1 {
				t.Fatal("valid prefix lost or limit exceeded")
			}
		})
	}
}

func TestFileBatchPartialWriteHasNoAcceptedAggregates(t *testing.T) {
	sink := &batchSink{fail: true}
	o := batchOutput(t, trace.DefaultBounds(), trace.ConfirmedPaths, sink)
	if err := o.FileActivities(batchEvents(o, "/fixture")); err == nil || !o.transportFailed || o.events != 0 || o.aggregates.Observations() != 0 || o.bytes != 3 || sink.writes != 1 {
		t.Fatal("failed batch was accepted or retried")
	}
}

func TestFileBatchStopsAfterAuthorisationLossBetweenChunks(t *testing.T) {
	sink := &batchSink{}
	o := batchOutput(t, trace.DefaultBounds(), trace.ConfirmedPaths, sink)
	sink.cancel = o.cancel
	if err := o.FileActivities(batchEvents(o, strings.Repeat("\x1b", 256))); err == nil {
		t.Fatal("cancelled batch succeeded")
	}
	if sink.writes != 1 || o.events == 0 || o.events >= trace.MaxFileBatch {
		t.Fatal("batch delivered after permission loss")
	}
}

func TestFileBatchOmitPathsAndClosedOutput(t *testing.T) {
	sink := &batchSink{}
	o := batchOutput(t, trace.DefaultBounds(), trace.OmitPaths, sink)
	if err := o.FileActivities(batchEvents(o, "/private-fixture")); err != nil || sink.writes != 0 || o.events != 0 || o.aggregates.Observations() != trace.MaxFileBatch {
		t.Fatal("default batch exposed raw frames or lost aggregate evidence")
	}
	o.closed = true
	if err := o.FileActivities(batchEvents(o, "/private-fixture")); err != Stop(trace.Cancelled) || sink.writes != 0 {
		t.Fatal("closed output delivered observations")
	}
}

func TestFailedFileBatchCannotEmitTerminalSummary(t *testing.T) {
	calls := 0
	sink := sinkFunc(func(_ context.Context, data []byte) (int, error) {
		calls++
		if calls == 2 {
			return 3, io.ErrUnexpectedEOF
		}
		return len(data), nil
	})
	adapter := func(_ context.Context, spec trace.Specification, out trace.Output) (trace.Result, error) {
		path, _ := trace.NewSensitiveText("/fixture", spec.Bounds().PathBytes)
		event := trace.FileActivity{ObservedAt: time.Now().UTC(), Operation: trace.FileRead, Path: path}
		err := out.(trace.FileBatchOutput).FileActivities([]trace.FileActivity{event, event})
		return trace.Result{Version: trace.ContractVersion, Incomplete: true}, err
	}
	result := aggregateSession(t, trace.Files, trace.ConfirmedPaths, trace.DefaultBounds(), adapter, sink).Run(context.Background())
	if result.TerminalDelivered || result.Err == nil || result.Summary.WrittenEvents != 0 || result.Summary.Aggregates.Observations != 0 || calls != 2 {
		t.Fatal("failed batch appended a summary or claimed accepted observations")
	}
}

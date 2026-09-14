package tracesession

import (
	"bytes"
	"context"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestPeriodicRevocationStopsEngineAndSuppressesFinalObservations(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.Duration = 3 * time.Second
	sink := &bufferSink{}
	var checks atomic.Int32
	validate := func(context.Context) error {
		if checks.Add(1) > 1 {
			return Stop(trace.AuthorisationLost)
		}
		return nil
	}
	session := makeSession(t, bounds, observing(0), sink, validate)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := session.Run(ctx)
	if !result.TerminalDelivered || result.Summary.Termination != trace.AuthorisationLost || checks.Load() != 2 {
		t.Fatalf("revocation did not cancel: %+v", result)
	}
	if result.Summary.EngineCounts.Produced != nil || result.Summary.ObservationStartedAt != nil || !result.Summary.Incomplete {
		t.Fatal("revocation exposed final observations")
	}
}
func TestConcurrentCallbacksCannotExceedLimitOrWriteAfterTermination(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.Events = 4
	sink := &bufferSink{}
	var retained trace.Output
	adapter := func(ctx context.Context, _ trace.Specification, out trace.Output) (trace.Result, error) {
		retained = out
		var workers sync.WaitGroup
		for i := 0; i < 32; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				_ = out.FileActivity(trace.FileActivity{ObservedAt: time.Now().UTC(), Operation: trace.FileRead})
			}()
		}
		workers.Wait()
		return trace.Result{}, ctx.Err()
	}
	result := makeSession(t, bounds, adapter, sink, allow).Run(context.Background())
	if !result.TerminalDelivered || result.Summary.WrittenEvents != 4 || result.Summary.Termination != trace.EventLimit {
		t.Fatalf("concurrent ceiling: %+v", result)
	}
	before := sink.data.Len()
	if err := retained.FileActivity(trace.FileActivity{ObservedAt: time.Now().UTC(), Operation: trace.FileRead}); err == nil {
		t.Fatal("late callback accepted")
	}
	if sink.data.Len() != before {
		t.Fatal("late callback wrote past summary")
	}
}
func TestCancellationWaitsForEngineCleanupBeforeSummary(t *testing.T) {
	started, cleaned := make(chan struct{}), make(chan struct{})
	sink := sinkFunc(func(_ context.Context, data []byte) (int, error) {
		// A terminal write is distinguishable without retaining any event payload.
		if bytes.Contains(data, []byte(`"type":"summary"`)) {
			select {
			case <-cleaned:
			default:
				t.Error("summary preceded engine cleanup")
			}
		}
		return len(data), nil
	})
	adapter := func(ctx context.Context, _ trace.Specification, _ trace.Output) (trace.Result, error) {
		close(started)
		<-ctx.Done()
		close(cleaned)
		return trace.Result{}, ctx.Err()
	}
	session := makeSession(t, trace.DefaultBounds(), adapter, sink, allow)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan Outcome, 1)
	go func() { result <- session.Run(ctx) }()
	<-started
	cancel()
	select {
	case outcome := <-result:
		if !outcome.TerminalDelivered || outcome.Summary.Termination != trace.Cancelled {
			t.Fatalf("cancellation: %+v", outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not complete")
	}
}

func TestEarlierLeaseDeadlineBoundsSessionMetadataAndRuntime(t *testing.T) {
	sink := &bufferSink{}
	session := makeSession(t, trace.DefaultBounds(), observing(0), sink, allow)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	result := session.Run(ctx)
	if !result.TerminalDelivered || result.Summary.Termination != trace.Expired {
		t.Fatalf("lease deadline: %+v", result)
	}
	reader := traceframe.NewReader(bytes.NewReader(sink.data.Bytes()))
	for {
		_, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("lease-bounded metadata was invalid")
		}
	}
}

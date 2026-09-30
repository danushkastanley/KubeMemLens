package traceclient

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func streamFixture(t *testing.T, intent Intent, count int) ([]byte, []byte, []byte) {
	t.Helper()
	s := selectionFixture()
	target := trace.TargetIdentity{Namespace: s.Namespace, PodName: s.Pod, PodUID: s.PodUID, ContainerName: s.Container, ContainerID: s.ContainerID, ContainerStartedAt: s.ContainerStartedAt, NodeUID: "server-node-uid", CgroupID: 42}
	spec, err := trace.NewSpecification(intent.Kind, target, intent.Paths, intent.Bounds)
	if err != nil {
		t.Fatal(err)
	}
	doc := preflightFixture(t, s, intent)
	start := time.Now().UTC()
	end := start.Add(intent.Bounds.Duration)
	m, err := traceframe.NewMetadataVersion(traceframe.Metadata{SessionID: strings.Repeat("c", 32), EngineDigest: doc.Node.EngineDigest, ProgrammeDigest: doc.Node.ProgrammeDigest, Specification: spec, SessionStartedAt: start, Deadline: end}, 2)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := traceframe.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	var events bytes.Buffer
	agg, _ := traceaggregate.New(trace.Files, intent.Bounds.Events)
	for range count {
		path, _ := trace.NewSensitiveText("private-test-file", intent.Bounds.PathBytes)
		event := trace.FileActivity{ObservedAt: start.Add(time.Millisecond), Operation: trace.FileRead, Path: path}
		frame, err := traceframe.NewFileVersion(event, spec, 2)
		if err != nil {
			t.Fatal(err)
		}
		data, err := traceframe.Encode(frame)
		if err != nil {
			t.Fatal(err)
		}
		events.Write(data)
		if err := agg.File(event); err != nil {
			t.Fatal(err)
		}
	}
	produced, zero := uint64(count), uint64(0)
	snapshot := agg.Snapshot()
	last, err := traceframe.NewSummaryVersion(traceframe.Summary{SessionEndedAt: end, ObservationStartedAt: &start, ObservationEndedAt: &end, Termination: trace.Expired, EngineCounts: trace.Counts{Produced: &produced, Sampled: &zero, Lost: &zero, Rejected: &zero}, WrittenEvents: produced, WrittenBytesBeforeSummary: uint64(len(metadata) + events.Len()), Incomplete: true, Aggregates: &snapshot}, 2)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := traceframe.Encode(last)
	if err != nil {
		t.Fatal(err)
	}
	return metadata, events.Bytes(), summary
}

func watchAdmission(t *testing.T, c *Client, intent Intent) Admission {
	t.Helper()
	plan := planFixture(t, c, preflightFixture(t, selectionFixture(), intent))
	plan.intent = intent
	return Admission{client: c, namespace: "tenant-a", id: strings.Repeat("c", 32), engine: plan.document.Node.EngineDigest, state: "admitted", plan: &plan}
}

func TestWatchCoalescesMaximumBoundedStreamWithoutRetainingPaths(t *testing.T) {
	intent := DefaultIntent(trace.Files)
	intent.Bounds.Events = 100000
	intent.Bounds.OutputBytes = 32 << 20
	intent.Paths = trace.ConfirmedPaths
	metadata, events, summary := streamFixture(t, intent, 100000)
	var calls atomic.Int64
	c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.ProtoMajor != 1 {
			t.Error("activation stream did not use isolated HTTP/1 transport")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write(metadata)
		_, _ = w.Write(events)
		_, _ = w.Write(summary)
	}))
	updates := 0
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := c.Watch(ctx, watchAdmission(t, c, intent), func(Result) { updates++ })
	maximumUpdates := int(time.Since(start)/(100*time.Millisecond)) + 3
	if err != nil || !result.TransportComplete || result.DeliveredEvents != 100000 || updates > maximumUpdates || calls.Load() != 1 {
		t.Fatal("bounded stream or update cadence failed", err, updates, maximumUpdates)
	}
	s, ok := result.Summary()
	if !ok || s.Aggregates.Observations != 100000 {
		t.Fatal("numeric summary missing")
	}
	*s.EngineCounts.Produced = 999
	other, ok := result.Summary()
	if !ok || *other.EngineCounts.Produced != 100000 {
		t.Fatal("result retained mutable summary")
	}
	if strings.Contains(fmt.Sprintf("%+v", result), "private-test-file") {
		t.Fatal("formatted result leaked events")
	}
}

func TestWatchRejectsChangedTargetMissingTerminalAndTrailingData(t *testing.T) {
	intent := DefaultIntent(trace.Files)
	metadata, _, summary := streamFixture(t, intent, 0)
	for _, mode := range []string{"target", "missing", "trailing", "version"} {
		t.Run(mode, func(t *testing.T) {
			data := append(append([]byte{}, metadata...), summary...)
			switch mode {
			case "target":
				data = bytes.Replace(data, []byte("selected-uid"), []byte("replacement-uid"), 1)
			case "missing":
				data = metadata
			case "trailing":
				data = append(data, metadata...)
			case "version":
				data = bytes.Replace(data, []byte(`"version":2`), []byte(`"version":99`), 1)
			}
			c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write(data)
			}))
			result, err := c.Watch(context.Background(), watchAdmission(t, c, intent), nil)
			if err == nil || result.TransportComplete {
				t.Fatal("invalid or incomplete stream accepted")
			}
			if _, ok := result.Summary(); ok {
				t.Fatal("untrusted terminal result retained")
			}
			if mode == "target" && result.Metadata.PodUID != "" {
				t.Fatal("replacement target was disclosed")
			}
		})
	}
}

func TestWatchAllowsOnlyOneActiveReaderAndDoesNotReplayDroppedStream(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int64
	metadata, _, _ := streamFixture(t, DefaultIntent(trace.Files), 0)
	c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write(metadata)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	a := watchAdmission(t, c, DefaultIntent(trace.Files))
	go func() { _, err := c.Watch(ctx, a, nil); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	if _, err := c.Watch(context.Background(), a, nil); err == nil {
		t.Fatal("second reader started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("dropped stream reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not cancel")
	}
	if calls.Load() != 1 {
		t.Fatal("stream activation was replayed")
	}
}

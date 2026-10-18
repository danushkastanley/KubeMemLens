package admissionapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/streamhttp"
)

func relayBurst(t testing.TB) []byte {
	t.Helper()
	start, end := time.Unix(200, 0).UTC(), time.Unix(230, 0).UTC()
	target := trace.TargetIdentity{Namespace: "tenant", PodName: "pod", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(100, 0).UTC(), NodeUID: "node", CgroupID: 42}
	spec, err := trace.NewSpecification(trace.Files, target, trace.ConfirmedPaths, trace.DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	meta, err := traceframe.NewMetadataVersion(traceframe.Metadata{SessionID: strings.Repeat("b", 32), EngineDigest: streamDigest, ProgrammeDigest: streamDigest, Specification: spec, SessionStartedAt: start, Deadline: end}, traceframe.AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(frame traceframe.Frame) []byte {
		data, err := traceframe.Encode(frame)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	data := encode(meta)
	path, err := trace.NewSensitiveText("/work/fixed-seed.bin", 256)
	if err != nil {
		t.Fatal(err)
	}
	amount := uint64(65536)
	event := trace.FileActivity{ObservedAt: start.Add(time.Second), Operation: trace.FileRead, RequestedBytes: &amount, CompletedBytes: &amount, Path: path}
	frame, err := traceframe.NewFileVersion(event, spec, traceframe.AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err := traceaggregate.New(trace.Files, 10000)
	if err != nil {
		t.Fatal(err)
	}
	for range 128 {
		data = append(data, encode(frame)...)
		if err := aggregate.File(event); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := aggregate.Snapshot()
	produced, zero := uint64(128), uint64(0)
	last, err := traceframe.NewSummaryVersion(traceframe.Summary{SessionEndedAt: end, ObservationStartedAt: &start, ObservationEndedAt: &end, Termination: trace.Expired, EngineCounts: trace.Counts{Produced: &produced, Sampled: &zero, Lost: &zero, Rejected: &zero}, WrittenEvents: 128, WrittenBytesBeforeSummary: uint64(len(data)), Incomplete: true, Aggregates: &snapshot}, traceframe.AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, encode(last)...)
}

type batchResponse struct {
	*httptest.ResponseRecorder
	writes, flushes, maxBytes int
	failure                   string
}

func (*batchResponse) SetWriteDeadline(time.Time) error { return nil }
func (w *batchResponse) FlushError() error {
	w.flushes++
	if w.failure == "flush" {
		return errors.New("flush failed")
	}
	return nil
}
func (w *batchResponse) Write(data []byte) (int, error) {
	w.writes++
	w.maxBytes = max(w.maxBytes, len(data))
	if w.failure == "short" {
		return w.ResponseRecorder.Write(data[:1])
	}
	return w.ResponseRecorder.Write(data)
}

func batchRelay(t testing.TB, data []byte, output *batchResponse) (*relay, traceframe.Frame) {
	t.Helper()
	sink, err := streamhttp.NewSink(output)
	if err != nil {
		t.Fatal(err)
	}
	r := &relay{reader: traceframe.NewReader(bytes.NewReader(data)), sink: sink}
	meta, err := r.reader.Next()
	if err != nil || r.forward(context.Background(), meta) != nil {
		t.Fatal("metadata failed")
	}
	first, err := r.reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	return r, first
}

func TestRelayBatchPreservesEveryWireByteAndCount(t *testing.T) {
	data := relayBurst(t)
	out := &batchResponse{ResponseRecorder: httptest.NewRecorder()}
	r, next := batchRelay(t, data, out)
	for next.Type() == traceframe.EventFrame {
		var err error
		next, err = r.forwardEvents(t.Context(), next)
		if err != nil {
			t.Fatal(err)
		}
		if r.batch != [traceframe.MaxBytes]byte{} {
			t.Fatal("batch retained private bytes")
		}
	}
	if next.Type() != traceframe.SummaryFrame || r.forward(t.Context(), next) != nil {
		t.Fatal("terminal frame changed")
	}
	if _, err := r.reader.Next(); err != io.EOF {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Body.Bytes(), data) || r.events != 128 || r.written != uint64(len(data)) {
		t.Fatal("batch changed wire bytes or accounting")
	}
	if out.flushes != out.writes || out.writes > 7 || out.maxBytes > traceframe.MaxBytes {
		t.Fatalf("unbounded or uncoalesced writes: %d", out.writes)
	}
}

func TestRelayBatchFailureCannotCommitEventsOrAppendSummary(t *testing.T) {
	for _, failure := range []string{"short", "flush", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			out := &batchResponse{ResponseRecorder: httptest.NewRecorder()}
			r, first := batchRelay(t, relayBurst(t), out)
			out.failure = failure
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if failure == "cancelled" {
				cancel()
			}
			if _, err := r.forwardEvents(ctx, first); err == nil || !r.transportFailed || r.events != 0 {
				t.Fatal("failed batch counted as delivered")
			}
			before := out.Body.Len()
			if err := r.terminate(t.Context(), trace.EngineFailed); err == nil || out.Body.Len() != before {
				t.Fatal("summary appended after failed batch")
			}
			if r.batch != [traceframe.MaxBytes]byte{} {
				t.Fatal("failed batch retained private bytes")
			}
		})
	}
}

func TestRelayRejectsTrailingFrameBeforeForwardingSummary(t *testing.T) {
	data := append(relayBurst(t), []byte("{}\n")...)
	out := &batchResponse{ResponseRecorder: httptest.NewRecorder()}
	sink, err := streamhttp.NewSink(out)
	if err != nil {
		t.Fatal(err)
	}
	r := &relay{reader: traceframe.NewReader(bytes.NewReader(data)), sink: sink}
	meta, err := r.reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.run(t.Context(), meta); err == nil {
		t.Fatal("trailing frame accepted")
	}
	if r.events != 128 || bytes.Contains(out.Body.Bytes(), []byte(`"type":"summary"`)) {
		t.Fatal("terminal bypassed EOF validation")
	}
}

func TestRelayPreservesValidPrefixBeforeMalformedBufferedEvent(t *testing.T) {
	lines := bytes.SplitAfter(relayBurst(t), []byte("\n"))
	prefix := bytes.Join(lines[:4], nil) // Metadata followed by three valid events.
	data := append(bytes.Clone(prefix), []byte("{}\n")...)
	out := &batchResponse{ResponseRecorder: httptest.NewRecorder()}
	r, first := batchRelay(t, data, out)
	if _, err := r.forwardEvents(t.Context(), first); err == nil {
		t.Fatal("malformed buffered event accepted")
	}
	if r.transportFailed || r.events != 3 || r.written != uint64(len(prefix)) || !bytes.Equal(out.Body.Bytes(), prefix) {
		t.Fatal("validated prefix or known delivery accounting lost")
	}
	if r.batch != [traceframe.MaxBytes]byte{} {
		t.Fatal("rejected upstream frame retained private bytes")
	}
}

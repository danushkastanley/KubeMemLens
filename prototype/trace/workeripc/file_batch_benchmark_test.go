package workeripc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/internal/tracesession"
	"github.com/danushkastanley/kube-memlens/prototype/trace/streamhttp"
)

type burstAdapter struct{ mode string }

func (a burstAdapter) Run(ctx context.Context, spec trace.Specification, out trace.Output) (trace.Result, error) {
	start := time.Now().UTC()
	deadline, _ := ctx.Deadline()
	request := Request{spec, start, deadline, strings.Repeat("b", 64)}
	var data bytes.Buffer
	writer, err := NewWriter(&data, request)
	if err != nil {
		return trace.Result{}, err
	}
	if err := writer.Ready(); err != nil {
		return trace.Result{}, err
	}
	requested, completed := uint64(4096), uint64(128)
	path, _ := trace.NewSensitiveText("/fixture", spec.Bounds().PathBytes)
	for range 128 {
		if err := writer.FileActivity(trace.FileActivity{ObservedAt: start, Operation: trace.FileRead, RequestedBytes: &requested, CompletedBytes: &completed, Path: path}); err != nil {
			return trace.Result{}, err
		}
	}
	count, zero := uint64(128), uint64(0)
	result := trace.Result{Version: trace.ContractVersion, StartedAt: start, EndedAt: time.Now().UTC(), Termination: trace.Cancelled, Counts: trace.Counts{Produced: &count, Sampled: &zero, Lost: &zero, Rejected: &zero}, Incomplete: true}
	if err := writer.Finish(result); err != nil {
		return trace.Result{}, err
	}
	reader, pipe, err := os.Pipe()
	if err != nil {
		return trace.Result{}, err
	}
	done := make(chan error, 1)
	go func() {
		_, err := pipe.Write(data.Bytes())
		closeErr := pipe.Close()
		if err == nil {
			err = closeErr
		}
		done <- err
	}()
	if a.mode == "per-event" {
		out = struct{ trace.Output }{out}
	}
	result, readErr := ReadStream(reader, request, out, func() {})
	closeErr, writeErr := reader.Close(), <-done
	if readErr != nil {
		return trace.Result{}, readErr
	}
	if closeErr != nil {
		return trace.Result{}, closeErr
	}
	if writeErr != nil {
		return trace.Result{}, writeErr
	}
	return result, nil
}

type batchFlushCounter struct {
	http.ResponseWriter
	count *atomic.Int64
}

func (w batchFlushCounter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w batchFlushCounter) FlushError() error {
	w.count.Add(1)
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Exercise private OS-pipe decoding, real session accounting and real TLS writes.
// The per-event mode masks only the optional batch interface on the same output.
// This component benchmark is not kernel or whole-system qualification.
func BenchmarkNodeFileTLSBurst(b *testing.B) {
	spec := requestFixture(b, trace.Files, trace.ConfirmedPaths, trace.DefaultBounds()).Specification
	for _, mode := range []string{"per-event", "buffered-files"} {
		b.Run(mode, func(b *testing.B) {
			var flushes atomic.Int64
			outcomes := make(chan tracesession.Outcome, 1)
			engine, _ := trace.NewEngine(burstAdapter{mode})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sink, err := streamhttp.NewSink(batchFlushCounter{w, &flushes})
				if err != nil {
					outcomes <- tracesession.Outcome{Err: err}
					return
				}
				metadata := traceframe.Metadata{SessionID: strings.Repeat("b", 32), EngineDigest: "sha256:" + strings.Repeat("c", 64), ProgrammeDigest: "sha256:" + strings.Repeat("d", 64), Specification: spec}
				session, err := tracesession.NewVersion(metadata, engine, sink, func(context.Context) error { return nil }, traceframe.AggregateVersion)
				if err != nil {
					outcomes <- tracesession.Outcome{Err: err}
					return
				}
				outcomes <- session.Run(r.Context())
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = 5 * time.Second
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				response, err := client.Get(server.URL)
				if err != nil {
					b.Fatal(err)
				}
				data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
				closeErr := response.Body.Close()
				if err != nil || closeErr != nil || response.StatusCode != 200 {
					b.Fatal("incomplete TLS response")
				}
				outcome := <-outcomes
				if outcome.Err != nil || !outcome.TerminalDelivered || outcome.Summary.WrittenEvents != 128 || outcome.Summary.Aggregates == nil || outcome.Summary.Aggregates.Observations != 128 {
					b.Fatal("lost burst accounting")
				}
				if err := validateTLSBurst(data); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(flushes.Load())/float64(b.N), "flushes/burst")
		})
	}
}
func validateTLSBurst(data []byte) error {
	reader := traceframe.NewReader(bytes.NewReader(data))
	for index := range 130 {
		frame, err := reader.Next()
		if err != nil {
			return err
		}
		want := traceframe.EventFrame
		if index == 0 {
			want = traceframe.MetadataFrame
		}
		if index == 129 {
			want = traceframe.SummaryFrame
		}
		if frame.Type() != want {
			return fmt.Errorf("invalid frame order")
		}
	}
	if _, err := reader.Next(); err != io.EOF {
		return fmt.Errorf("missing terminal EOF")
	}
	return nil
}

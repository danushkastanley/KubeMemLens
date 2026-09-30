package admissionapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/streamhttp"
)

type flushCounter struct {
	http.ResponseWriter
	count *atomic.Int64
}

func (w flushCounter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w flushCounter) FlushError() error {
	w.count.Add(1)
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Compare synchronous per-frame and buffered-event forwarding over real TLS.
// Both paths use the current production decoder and identical immutable bytes.
func BenchmarkRelayTLSBurst(b *testing.B) {
	data := relayBurst(b)
	for _, mode := range []string{"per-frame", "buffered-events"} {
		b.Run(mode, func(b *testing.B) {
			var flushes atomic.Int64
			failures := make(chan error, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if err := benchmarkRelay(req.Context(), flushCounter{w, &flushes}, data, mode); err != nil {
					select {
					case failures <- err:
					default:
					}
				}
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = 5 * time.Second
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				response, err := client.Get(server.URL)
				if err != nil {
					b.Fatal(err)
				}
				n, err := io.Copy(io.Discard, response.Body)
				closeErr := response.Body.Close()
				if err != nil || closeErr != nil || n != int64(len(data)) || response.StatusCode != 200 {
					b.Fatal("incomplete TLS burst")
				}
				select {
				case err := <-failures:
					b.Fatal(err)
				default:
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(flushes.Load())/float64(b.N), "flushes/burst")
		})
	}
}

func benchmarkRelay(ctx context.Context, w http.ResponseWriter, data []byte, mode string) error {
	sink, err := streamhttp.NewSink(w)
	if err != nil {
		return err
	}
	r := &relay{reader: traceframe.NewReader(bytes.NewReader(data)), sink: sink}
	frame, err := r.reader.Next()
	for err == nil {
		if mode == "buffered-events" && frame.Type() == traceframe.EventFrame {
			frame, err = r.forwardEvents(ctx, frame)
			continue
		}
		if err := r.forward(ctx, frame); err != nil {
			return err
		}
		frame, err = r.reader.Next()
	}
	if err != io.EOF {
		return err
	}
	return nil
}

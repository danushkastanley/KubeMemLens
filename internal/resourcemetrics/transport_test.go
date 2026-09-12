package resourcemetrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func TestCancellationAndTimeoutUseTheRealTransport(t *testing.T) {
	entered := make(chan struct{}, 1)
	source := newTestSource(t, func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() }, Options{Timeout: time.Second})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := source.Read(ctx); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	slow := newTestSource(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, Options{Timeout: 20 * time.Millisecond})
	report, err := slow.Read(t.Context())
	if !errors.Is(err, context.DeadlineExceeded) || report.Availability != Unavailable {
		t.Fatalf("timeout=%+v %v", report, err)
	}
	if strings.Contains(err.Error(), "http://") {
		t.Fatal("transport failure exposed API URL")
	}
}

func TestRedirectsNeverForwardCallerCredentials(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	source := newTestSource(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }, Options{})
	report, err := source.Read(t.Context())
	if err != nil || report.Availability != Unavailable || forwarded.Load() != 0 {
		t.Fatalf("redirect=%+v %v forwarded=%d", report, err, forwarded.Load())
	}
}

func TestClientPreservesConfigurationAndSupportsConcurrentReads(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(apiHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		jsonResponse(t, w, listBody("v1", metricPod("app", sampleTime, map[string]string{"cpu": "1m", "memory": "1Ki"})))
	}))
	defer server.Close()
	config := &rest.Config{Host: server.URL, BearerToken: "caller-token"}
	source, err := New(config, Options{Namespace: "team-a", Now: func() time.Time { return sampleTime }})
	if err != nil {
		t.Fatal(err)
	}
	if config.DisableCompression {
		t.Fatal("constructor mutated caller configuration")
	}
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			report, err := source.Read(t.Context())
			if err != nil || report.Availability != Available {
				t.Errorf("read=%+v %v", report, err)
			}
		}()
	}
	group.Wait()
	if requests.Load() != 12 {
		t.Fatal(requests.Load())
	}
}

func TestTruncatedOrTrailingJSONNeverReturnsRows(t *testing.T) {
	for _, body := range []string{`{"name":"metrics.k8s.io"`, `{"name":"metrics.k8s.io"} {}`, `null`} {
		source := newTestSource(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }, Options{})
		report, _ := source.Read(t.Context())
		if report.Availability != Unavailable || len(report.Observations) != 0 {
			t.Fatal(report)
		}
	}
}

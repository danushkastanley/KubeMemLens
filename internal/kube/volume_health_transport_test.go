package kube

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"k8s.io/client-go/rest"
)

func TestHealthTransportBounds(t *testing.T) {
	for _, body := range []string{`{`, `{} {}`, strings.Repeat(" ", maxHealthResponse+1)} {
		r, err := testHealthReader(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }, VolumeHealthOptions{}).Query(context.Background(), "workload")
		if err == nil || r.Summary().Observations != 0 {
			t.Fatal("malformed/oversized response accepted")
		}
	}
	f := newHealthFixture()
	f.pvc.Annotations = map[string]string{"padding": strings.Repeat("a", 700<<10)}
	first := f.pod.Spec.Volumes[0]
	for i := 0; i < 20; i++ {
		v := first
		v.Name = fmt.Sprintf("data-%d", i)
		f.pod.Spec.Volumes = append(f.pod.Spec.Volumes, v)
	}
	r, err := testHealthReader(t, f.handler(t, ""), VolumeHealthOptions{}).Query(context.Background(), "workload")
	if err == nil || r.Summary().Observations != 0 {
		t.Fatal("aggregate query byte ceiling not enforced")
	}
}

func TestHealthCancellationTimeoutAndRedirect(t *testing.T) {
	blocked := func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }
	reader := testHealthReader(t, blocked, VolumeHealthOptions{Timeout: 20 * time.Millisecond})
	_, err := reader.Query(context.Background(), "workload")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reader.Query(ctx, "workload")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Store(true) }))
	defer target.Close()
	reader = testHealthReader(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }, VolumeHealthOptions{})
	_, err = reader.Query(context.Background(), "workload")
	if err == nil || followed.Load() {
		t.Fatal("redirect followed or reported success")
	}
}

func TestHealthRevocationAndConcurrentQueries(t *testing.T) {
	f := newHealthFixture()
	h := f.handler(t, "")
	var revoked atomic.Bool
	reader := testHealthReader(t, func(w http.ResponseWriter, r *http.Request) {
		if revoked.Load() {
			http.Error(w, "private", 403)
			return
		}
		h(w, r)
	}, VolumeHealthOptions{})
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Go(func() {
			r, err := reader.Query(context.Background(), "workload")
			if err != nil || r.Summary().Observations != 3 {
				t.Errorf("concurrent query: %v %v", r, err)
			}
		})
	}
	group.Wait()
	revoked.Store(true)
	r, err := reader.Query(context.Background(), "workload")
	var readErr *HealthReadError
	if !errors.As(err, &readErr) || readErr.Reason != volumehealth.AccessDenied || r.Summary().Observations != 0 {
		t.Fatal("revoked query retained prior caller data")
	}
}

func TestHealthConfigurationAndQueryValidation(t *testing.T) {
	for _, host := range []string{"", "://", "ftp://example.test", "https://user:secret@example.test", "https://example.test?q=1"} {
		if _, err := NewVolumeHealthSource(&rest.Config{Host: host}, VolumeHealthOptions{Namespace: "team-a"}); err == nil {
			t.Errorf("accepted host %q", host)
		}
	}
	if _, err := NewVolumeHealthSource(nil, VolumeHealthOptions{}); err == nil {
		t.Fatal("nil config accepted")
	}
	for _, opts := range []VolumeHealthOptions{{Namespace: "../team-b"}, {Namespace: "team-a", Timeout: -1}, {Namespace: "team-a", Timeout: 2 * time.Minute}} {
		if _, err := NewVolumeHealthSource(&rest.Config{Host: "https://example.test"}, opts); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
	config := &rest.Config{Host: "http://example.test", Timeout: time.Second}
	reader, err := NewVolumeHealthSource(config, VolumeHealthOptions{Namespace: "team-a"})
	if err != nil {
		t.Fatal(err)
	}
	if config.DisableCompression || config.Timeout != time.Second {
		t.Fatal("caller configuration mutated")
	}
	for _, name := range []string{"", "../data", "team-b/data"} {
		if _, err := reader.Query(context.Background(), name); err == nil {
			t.Fatal("invalid Pod name accepted")
		}
	}
}

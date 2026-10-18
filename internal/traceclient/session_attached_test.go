package traceclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAttachedSessionNeverCreatesOrSelectsReplacement(t *testing.T) {
	for _, cancelBeforeStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "watch", true: "cancel"}[cancelBeforeStart], func(t *testing.T) {
			var streams, deletes atomic.Int64
			metadata, _, terminal := streamFixture(t, DefaultIntent(trace.Files), 0)
			id := strings.Repeat("c", 32)
			c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "DELETE":
					deletes.Add(1)
					_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusSuccess, Code: 200})
				case strings.HasSuffix(r.URL.Path, "/stream"):
					streams.Add(1)
					w.Header().Set("Content-Type", "application/x-ndjson")
					_, _ = w.Write(metadata)
					_, _ = w.Write(terminal)
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/traces/"+id):
					_ = json.NewEncoder(w).Encode(admittedFixture("tenant-a", id))
				default:
					t.Error("attached session attempted another selection/admission")
					http.NotFound(w, r)
				}
			}))
			a, err := c.Inspect(context.Background(), "tenant-a", id)
			if err != nil {
				t.Fatal(err)
			}
			s, err := NewAttachedSession(c, a)
			if err != nil {
				t.Fatal(err)
			}
			if cancelBeforeStart {
				s.Cancel()
			} else if err := s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			result := waitSession(t, s)
			if result.Cleanup != CleanupConfirmed || deletes.Load() != 1 {
				t.Fatal("missing cleanup")
			}
			if cancelBeforeStart {
				if result.State != StateCancelled || streams.Load() != 0 {
					t.Fatal("cancel activated stream")
				}
			} else if result.State != StateCompleted || streams.Load() != 1 || result.Result.Metadata.PodUID != "selected-uid" {
				t.Fatal("recovered session lost server identity", result.Failure)
			}
		})
	}
}

func TestCancelDuringPreflightNeverAdmits(t *testing.T) {
	active := make(chan struct{})
	var creates atomic.Int64
	c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == apiPrefix:
			discoverFixture(w)
		case strings.HasSuffix(r.URL.Path, "/tracepreflights"):
			close(active)
			<-r.Context().Done()
		default:
			creates.Add(1)
			http.NotFound(w, r)
		}
	}))
	s, err := NewSession(c, selectionFixture(), DefaultIntent(trace.Files))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Prepare(context.Background()) }()
	select {
	case <-active:
	case <-time.After(time.Second):
		t.Fatal("preflight did not start")
	}
	s.Cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled preflight passed")
		}
	case <-time.After(time.Second):
		t.Fatal("preflight did not stop")
	}
	value := waitSession(t, s)
	if value.State != StateCancelled || value.Cleanup != CleanupNotRequested || creates.Load() != 0 {
		t.Fatal("preflight cancellation admitted")
	}
}

func TestConfirmedCancelBeforeStreamMetadataKeepsEvidenceIncomplete(t *testing.T) {
	for _, kind := range []ErrorKind{Gone, Unavailable, Incomplete} {
		state, err := cancelledResult(Result{}, failure(kind), CleanupConfirmed, nil)
		if state != StateCancelled || err == nil {
			t.Fatal("confirmed early cancellation lost uncertainty", kind, state)
		}
		state, _ = cancelledResult(Result{}, failure(kind), CleanupUnconfirmed, failure(Denied))
		if state != StateFailed {
			t.Fatal("unconfirmed early cancellation became success", kind)
		}
	}
	for _, kind := range []ErrorKind{Protocol, TargetChanged, Denied} {
		state, _ := cancelledResult(Result{}, failure(kind), CleanupConfirmed, nil)
		if state != StateFailed {
			t.Fatal("cancellation masked invalid evidence", kind)
		}
	}
}

func TestSessionCompensatesKnownAdmissionMismatchWithoutActivation(t *testing.T) {
	var creates, deletes, streams atomic.Int64
	doc := preflightFixture(t, selectionFixture(), DefaultIntent(trace.Files))
	c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == apiPrefix:
			discoverFixture(w)
		case strings.HasSuffix(r.URL.Path, "/tracepreflights"):
			_ = json.NewEncoder(w).Encode(doc)
		case r.Method == "POST":
			creates.Add(1)
			a := admittedFixture("tenant-a", strings.Repeat("c", 32))
			a.EngineDigest = "sha256:" + strings.Repeat("d", 64)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(a)
		case r.Method == "DELETE":
			deletes.Add(1)
			_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusSuccess, Code: 200})
		default:
			streams.Add(1)
			http.NotFound(w, r)
		}
	}))
	s, err := NewSession(c, selectionFixture(), DefaultIntent(trace.Files))
	if err != nil {
		t.Fatal(err)
	}
	prepareSession(t, s)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	value := waitSession(t, s)
	if value.State != StateFailed || value.Cleanup != CleanupConfirmed || creates.Load() != 1 || deletes.Load() != 1 || streams.Load() != 0 {
		t.Fatal("mismatched admission was activated or left without cleanup")
	}
}

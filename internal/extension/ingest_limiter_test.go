package extension

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/collector"
)

// An agent makes four attempts inside one second. Attempts turned away by the
// concurrency gate must not spend the Node's rate allowance, or the later
// attempts are refused even after the collector has room again.
func TestConcurrencyRejectionsDoNotSpendTheRateAllowance(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	coordinator := testCoordinator(t, collector.NewStore(), now, 10)
	handler, err := NewHandler(coordinator, HandlerOptions{
		AgentUsername:    "system:serviceaccount:kube-memlens:kube-memlens-agent",
		MaxSnapshotBytes: 1 << 20, MaxConcurrent: 1, RequestsPerSec: 1, Burst: 2, MaxIdentities: 20,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	mux := newTestRouteMux()
	handler.Register(mux)

	holderBody, _ := json.Marshal(testRequest(now, "epoch-a", 1, "node-b", "node-uid-b"))
	holder := &blockingReader{reader: bytes.NewReader(holderBody), started: make(chan struct{}), release: make(chan struct{})}
	holderRequest := httptest.NewRequest(http.MethodPost, "/apis/memory.kubememlens.io/v1alpha1/nodesnapshots", holder)
	holderRequest.Header.Set("Content-Type", "application/json")
	holderRequest = withClaims(holderRequest, testClaims("pod-b", "node-b", "node-uid-b"))
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		mux.ServeHTTP(httptest.NewRecorder(), holderRequest)
	}()
	<-holder.started

	claims := testClaims("pod-a", "node-a", "node-uid-a")
	request := testRequest(now, "epoch-a", 1, "node-a", "node-uid-a")
	for attempt := 1; attempt <= 3; attempt++ {
		recorder := serveSnapshot(t, mux, claims, request, "")
		if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "concurrency_limited") {
			t.Fatalf("attempt %d while the collector is busy: status=%d body=%s, want 429 concurrency_limited",
				attempt, recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("Retry-After") != "1" {
			t.Fatalf("attempt %d: Retry-After = %q, want 1", attempt, recorder.Header().Get("Retry-After"))
		}
	}
	close(holder.release)
	<-holderDone

	fourth := serveSnapshot(t, mux, claims, request, "")
	if fourth.Code != http.StatusOK {
		t.Fatalf("fourth attempt after the collector freed up: status=%d body=%s, want 200", fourth.Code, fourth.Body.String())
	}
}

func TestRateLimitStillAppliesToAdmittedRequests(t *testing.T) {
	handler, _, now := testHandler(t, 1<<20, 1, 2)
	claims := testClaims("pod-a", "node-a", "node-uid-a")
	for sequence := uint64(1); sequence <= 2; sequence++ {
		recorder := serveSnapshot(t, handler, claims, testRequest(now, "epoch-a", sequence, "node-a", "node-uid-a"), "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d: status=%d body=%s, want 200", sequence, recorder.Code, recorder.Body.String())
		}
	}
	third := serveSnapshot(t, handler, claims, testRequest(now, "epoch-a", 3, "node-a", "node-uid-a"), "")
	if third.Code != http.StatusTooManyRequests || !strings.Contains(third.Body.String(), "rate_limited") {
		t.Fatalf("third request: status=%d body=%s, want 429 rate_limited", third.Code, third.Body.String())
	}
	if third.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", third.Header().Get("Retry-After"))
	}
}

// The cgroup agent and the Node-context producer run on the same Node, so
// they share a Node UID. Each needs its own allowance.
func TestProducersOnOneNodeHaveSeparateRateAllowances(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	handler, err := NewHandler(testCoordinator(t, collector.NewStore(), now, 10), HandlerOptions{
		AgentUsername:    "system:serviceaccount:kube-memlens:kube-memlens-agent",
		MaxSnapshotBytes: 1 << 20, MaxConcurrent: 1, RequestsPerSec: 1, Burst: 1, MaxIdentities: 20,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	cgroup := AgentClaims{Role: CgroupProducer, NodeUID: "node-uid-a"}
	nodeContext := AgentClaims{Role: NodeContextProducer, NodeUID: "node-uid-a"}
	if !handler.allowProducer(cgroup, now) {
		t.Fatal("first cgroup snapshot was rate limited")
	}
	if !handler.allowProducer(nodeContext, now) {
		t.Fatal("Node-context snapshot was rate limited by the cgroup agent's allowance")
	}
	if handler.allowProducer(cgroup, now) || handler.allowProducer(nodeContext, now) {
		t.Fatal("a producer exceeded its own allowance")
	}
}

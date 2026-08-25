package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSnapshotPublisherUsesAggregatedPathsAndIncreasingSequence(t *testing.T) {
	var mu sync.Mutex
	sequences := []uint64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1/ingestionepochs/current":
			writePublisherJSON(t, w, api.IngestionEpoch{
				TypeMeta:   metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IngestionEpoch"},
				ObjectMeta: metav1.ObjectMeta{Name: "current"}, Epoch: "epoch-a", SchemaVersion: 1,
			})
		case "/apis/memory.kubememlens.io/v1alpha1/nodesnapshots":
			var request api.NodeSnapshotRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode request: %v", err)
			}
			mu.Lock()
			sequences = append(sequences, request.Sequence)
			mu.Unlock()
			writePublisherJSON(t, w, validPublisherResponse(false))
		default:
			t.Errorf("unexpected request: method=%s path=%s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	publisher, err := newSnapshotPublisher(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("newSnapshotPublisher: %v", err)
	}
	snapshot := publisherSnapshot()
	if err := publisher.Publish(t.Context(), "node-uid-a", snapshot); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	snapshot.CapturedAt = snapshot.CapturedAt.Add(time.Second)
	if err := publisher.Publish(t.Context(), "node-uid-a", snapshot); err != nil {
		t.Fatalf("second Publish: %v", err)
	}
	if len(sequences) != 2 || sequences[0] != 1 || sequences[1] != 2 {
		t.Fatalf("sequences = %v, want [1 2]", sequences)
	}
}

func TestSnapshotPublisherRefreshesEpochWithoutResettingSequence(t *testing.T) {
	epochGets := 0
	posts := []api.NodeSnapshotRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			epochGets++
			epoch := "epoch-old"
			lastSequence := uint64(40)
			if epochGets > 1 {
				epoch = "epoch-new"
				lastSequence = 0
			}
			writePublisherJSON(t, w, api.IngestionEpoch{
				TypeMeta:   metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IngestionEpoch"},
				ObjectMeta: metav1.ObjectMeta{Name: "current"}, Epoch: epoch, LastSequence: lastSequence,
			})
			return
		}
		var request api.NodeSnapshotRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		posts = append(posts, request)
		if request.Epoch == "epoch-old" {
			w.WriteHeader(http.StatusConflict)
			writePublisherJSON(t, w, metav1.Status{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusFailure,
				Reason: "epoch_mismatch", Code: http.StatusConflict,
			})
			return
		}
		writePublisherJSON(t, w, validPublisherResponse(false))
	}))
	defer server.Close()
	publisher, _ := newSnapshotPublisher(server.Client(), server.URL)
	if err := publisher.Publish(t.Context(), "node-uid-a", publisherSnapshot()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(posts) != 2 || posts[0].Sequence != 41 || posts[1].Sequence != 41 || posts[1].Epoch != "epoch-new" {
		t.Fatalf("posts = %#v", posts)
	}
}

func TestSnapshotPublisherDoesNotRetryUnauthorized(t *testing.T) {
	posts := []api.NodeSnapshotRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writePublisherJSON(t, w, api.IngestionEpoch{
				TypeMeta:   metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IngestionEpoch"},
				ObjectMeta: metav1.ObjectMeta{Name: "current"}, Epoch: "epoch-a",
			})
			return
		}
		var request api.NodeSnapshotRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		posts = append(posts, request)
		w.WriteHeader(http.StatusUnauthorized)
		writePublisherJSON(t, w, metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Reason: metav1.StatusReasonUnauthorized, Code: http.StatusUnauthorized})
	}))
	defer server.Close()
	publisher, _ := newSnapshotPublisher(server.Client(), server.URL)
	if err := publisher.Publish(t.Context(), "node-uid-a", publisherSnapshot()); err == nil {
		t.Fatal("Publish returned nil error for an unauthorised request")
	}
	if len(posts) != 1 {
		t.Fatalf("unauthorised request attempts = %d, want 1", len(posts))
	}
}

func TestSnapshotPublisherRetriesTransientStatusesWithExactRequest(t *testing.T) {
	posts := []api.NodeSnapshotRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writePublisherJSON(t, w, api.IngestionEpoch{
				TypeMeta:   metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IngestionEpoch"},
				ObjectMeta: metav1.ObjectMeta{Name: "current"}, Epoch: "epoch-a",
			})
			return
		}
		var request api.NodeSnapshotRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		posts = append(posts, request)
		switch len(posts) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
			writePublisherJSON(t, w, metav1.Status{Reason: metav1.StatusReasonTooManyRequests, Code: http.StatusTooManyRequests})
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
			writePublisherJSON(t, w, metav1.Status{Reason: metav1.StatusReasonServiceUnavailable, Code: http.StatusServiceUnavailable})
		default:
			writePublisherJSON(t, w, validPublisherResponse(false))
		}
	}))
	defer server.Close()
	publisher, _ := newSnapshotPublisher(server.Client(), server.URL)
	delays := []time.Duration{}
	publisher.retry = deterministicRetryPolicy(&delays)
	if err := publisher.Publish(t.Context(), "node-uid-a", publisherSnapshot()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(posts) != 3 || posts[0].Sequence != posts[1].Sequence || posts[1].Sequence != posts[2].Sequence {
		t.Fatalf("transient retries changed sequence: %#v", posts)
	}
	for index := 1; index < len(posts); index++ {
		if posts[index].Epoch != posts[0].Epoch || !posts[index].Snapshot.CapturedAt.Equal(posts[0].Snapshot.CapturedAt) {
			t.Fatalf("transient retry changed request: %#v", posts)
		}
	}
	if len(delays) != 2 || delays[0] != 10*time.Millisecond || delays[1] != 20*time.Millisecond {
		t.Fatalf("retry delays = %v, want [10ms 20ms]", delays)
	}
}

func TestSnapshotPublisherRetriesTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writePublisherJSON(t, w, api.IngestionEpoch{
				TypeMeta:   metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IngestionEpoch"},
				ObjectMeta: metav1.ObjectMeta{Name: "current"}, Epoch: "epoch-a",
			})
			return
		}
		writePublisherJSON(t, w, validPublisherResponse(false))
	}))
	defer server.Close()
	baseTransport := server.Client().Transport
	postAttempts := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			postAttempts++
			if postAttempts == 1 {
				return nil, io.ErrUnexpectedEOF
			}
		}
		return baseTransport.RoundTrip(request)
	})}
	publisher, _ := newSnapshotPublisher(client, server.URL)
	publisher.retry = deterministicRetryPolicy(&[]time.Duration{})
	if err := publisher.Publish(t.Context(), "node-uid-a", publisherSnapshot()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if postAttempts != 2 {
		t.Fatalf("POST attempts = %d, want 2", postAttempts)
	}
}

func TestSnapshotPublisherRetriesTransientEpochRead(t *testing.T) {
	epochAttempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			epochAttempts++
			if epochAttempts < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				writePublisherJSON(t, w, metav1.Status{Reason: metav1.StatusReasonServiceUnavailable, Code: http.StatusServiceUnavailable})
				return
			}
			writePublisherJSON(t, w, api.IngestionEpoch{
				TypeMeta:   metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IngestionEpoch"},
				ObjectMeta: metav1.ObjectMeta{Name: "current"}, Epoch: "epoch-a",
			})
			return
		}
		writePublisherJSON(t, w, validPublisherResponse(false))
	}))
	defer server.Close()
	publisher, _ := newSnapshotPublisher(server.Client(), server.URL)
	delays := []time.Duration{}
	publisher.retry = deterministicRetryPolicy(&delays)
	if err := publisher.Publish(t.Context(), "node-uid-a", publisherSnapshot()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if epochAttempts != 3 || len(delays) != 2 {
		t.Fatalf("epoch attempts=%d delays=%v, want 3 attempts and 2 delays", epochAttempts, delays)
	}
}

func TestSnapshotPublisherBackoffHonoursCancellation(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writePublisherJSON(t, w, api.IngestionEpoch{
				TypeMeta:   metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IngestionEpoch"},
				ObjectMeta: metav1.ObjectMeta{Name: "current"}, Epoch: "epoch-a",
			})
			return
		}
		posts++
		w.WriteHeader(http.StatusServiceUnavailable)
		writePublisherJSON(t, w, metav1.Status{Reason: metav1.StatusReasonServiceUnavailable, Code: http.StatusServiceUnavailable})
	}))
	defer server.Close()
	publisher, _ := newSnapshotPublisher(server.Client(), server.URL)
	ctx, cancel := context.WithCancel(t.Context())
	publisher.retry = retryPolicy{
		maxAttempts: 4, baseDelay: time.Hour, maxDelay: time.Hour,
		jitter: func(delay time.Duration) time.Duration { return delay },
		wait: func(ctx context.Context, delay time.Duration) error {
			cancel()
			return waitForRetry(ctx, delay)
		},
	}
	err := publisher.Publish(ctx, "node-uid-a", publisherSnapshot())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Publish error = %v, want context cancellation", err)
	}
	if posts != 1 {
		t.Fatalf("POST attempts after cancellation = %d, want 1", posts)
	}
}

func TestSnapshotPublisherRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, (64<<10)+1))
	}))
	defer server.Close()
	publisher, _ := newSnapshotPublisher(server.Client(), server.URL)
	if err := publisher.Publish(t.Context(), "node-uid-a", publisherSnapshot()); err == nil {
		t.Fatal("Publish returned nil error for oversized response")
	}
}

func publisherSnapshot() api.AgentSnapshot {
	return api.AgentSnapshot{SchemaVersion: api.CurrentSnapshotSchemaVersion, NodeName: "node-a", CapturedAt: time.Now().UTC()}
}

func validPublisherResponse(duplicate bool) api.NodeSnapshotResponse {
	return api.NodeSnapshotResponse{
		TypeMeta: metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "NodeSnapshotResult"},
		Accepted: true, Duplicate: duplicate,
	}
}

func writePublisherJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func deterministicRetryPolicy(delays *[]time.Duration) retryPolicy {
	return retryPolicy{
		maxAttempts: 4, baseDelay: 10 * time.Millisecond, maxDelay: 20 * time.Millisecond,
		jitter: func(delay time.Duration) time.Duration { return delay },
		wait: func(_ context.Context, delay time.Duration) error {
			*delays = append(*delays, delay)
			return nil
		},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

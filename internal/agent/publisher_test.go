package agent

import (
	"encoding/json"
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

func TestSnapshotPublisherRetriesSameRequestAfterUnauthorized(t *testing.T) {
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
		if len(posts) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			writePublisherJSON(t, w, metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Reason: metav1.StatusReasonUnauthorized, Code: http.StatusUnauthorized})
			return
		}
		writePublisherJSON(t, w, validPublisherResponse(true))
	}))
	defer server.Close()
	publisher, _ := newSnapshotPublisher(server.Client(), server.URL)
	if err := publisher.Publish(t.Context(), "node-uid-a", publisherSnapshot()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(posts) != 2 || posts[0].Sequence != posts[1].Sequence || posts[0].Epoch != posts[1].Epoch {
		t.Fatalf("retry changed request: %#v", posts)
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

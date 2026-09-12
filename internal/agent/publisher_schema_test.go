package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func schemaEpoch(version int, epoch string) api.IngestionEpoch {
	return api.IngestionEpoch{
		TypeMeta:   metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IngestionEpoch"},
		ObjectMeta: metav1.ObjectMeta{Name: "current"}, Epoch: epoch, SchemaVersion: version,
	}
}

func schemaSnapshot() api.AgentSnapshot {
	snapshot := publisherSnapshot()
	snapshot.Containers = []api.ContainerSnapshot{{
		Memory:      model.MemoryBreakdown{IOPressure: model.IOPressure{State: model.IOAvailable}},
		ContainerID: "id-app", Context: api.ContainerContext{Resources: model.ContainerMemoryResources{
			Pod: model.PodMemoryResources{Configured: model.MemoryResourceBudget{Limit: model.ResourceValue{Bytes: 384, Known: true}}},
		}},
	}}
	return snapshot
}

func TestPublisherNegotiatesLegacyAndCurrentCollectors(t *testing.T) {
	for _, version := range []int{api.LegacySchemaVersion, 2, 3, 4, 5, api.CurrentSnapshotSchemaVersion} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			var posted api.NodeSnapshotRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get(api.SnapshotSchemaHeader) != fmt.Sprint(api.CurrentSnapshotSchemaVersion) {
					t.Error("publisher did not advertise its supported schema")
				}
				if r.Method == http.MethodGet {
					writePublisherJSON(t, w, schemaEpoch(version, "epoch-a"))
					return
				}
				if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
					t.Error(err)
				}
				writePublisherJSON(t, w, validPublisherResponse(false))
			}))
			defer server.Close()
			publisher, err := newSnapshotPublisher(server.Client(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := schemaSnapshot()
			if err := publisher.Publish(t.Context(), "node-a", snapshot); err != nil {
				t.Fatal(err)
			}
			if posted.Snapshot.SchemaVersion != version || len(posted.Snapshot.Containers) != 1 {
				t.Fatalf("wrong wire snapshot: %+v", posted.Snapshot)
			}
			if posted.Snapshot.Containers[0].Context.Resources.IsZero() != (version == api.LegacySchemaVersion) {
				t.Fatal("wire resource context did not match the negotiated schema")
			}
			if snapshot.Containers[0].Context.Resources.IsZero() {
				t.Fatal("compatibility projection mutated the scanner snapshot")
			}
			if (posted.Snapshot.Containers[0].Memory.IOPressure.State != "") != (version >= api.IOPressureSnapshotSchemaVersion) {
				t.Fatal("wire I/O context did not match the negotiated schema")
			}
		})
	}
}

func TestInvalidEpochSchemaDoesNotPoisonPublisherState(t *testing.T) {
	gets, posts := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets++
			version := api.CurrentSnapshotSchemaVersion
			if gets == 1 {
				version = 0
			}
			writePublisherJSON(t, w, schemaEpoch(version, "epoch-a"))
			return
		}
		posts++
		writePublisherJSON(t, w, validPublisherResponse(false))
	}))
	defer server.Close()
	publisher, err := newSnapshotPublisher(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), "node-a", schemaSnapshot()); err == nil || posts != 0 {
		t.Fatal("invalid epoch schema allowed a snapshot post")
	}
	if err := publisher.Publish(t.Context(), "node-a", schemaSnapshot()); err != nil {
		t.Fatal(err)
	}
	if gets != 2 || posts != 1 {
		t.Fatalf("invalid epoch was retained: gets=%d posts=%d", gets, posts)
	}
}

func TestEpochRefreshRestoresResourceContextAfterCollectorUpgrade(t *testing.T) {
	gets := 0
	var posts []api.NodeSnapshotRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets++
			writePublisherJSON(t, w, schemaEpoch(gets, "epoch"+strconv.Itoa(gets)))
			return
		}
		var request api.NodeSnapshotRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		posts = append(posts, request)
		if len(posts) == 1 {
			w.WriteHeader(http.StatusConflict)
			writePublisherJSON(t, w, metav1.Status{Reason: "epoch_mismatch", Code: http.StatusConflict})
			return
		}
		writePublisherJSON(t, w, validPublisherResponse(false))
	}))
	defer server.Close()
	publisher, err := newSnapshotPublisher(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), "node-a", schemaSnapshot()); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || posts[0].Sequence != posts[1].Sequence ||
		posts[0].Snapshot.SchemaVersion != 1 || posts[1].Snapshot.SchemaVersion != 2 ||
		!posts[0].Snapshot.Containers[0].Context.Resources.IsZero() || posts[1].Snapshot.Containers[0].Context.Resources.IsZero() {
		t.Fatalf("epoch refresh lost schema negotiation or retry identity: %+v", posts)
	}
}

func TestStrictCollectorRollbackRenegotiatesBeforeRetry(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(strconv.FormatBool(rollback), func(t *testing.T) {
			gets, posts := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					gets++
					version, epoch := 2, "new"
					if rollback && gets > 1 {
						version, epoch = 1, "old"
					}
					writePublisherJSON(t, w, schemaEpoch(version, epoch))
					return
				}
				posts++
				var request api.NodeSnapshotRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Snapshot.SchemaVersion == 2 {
					w.WriteHeader(http.StatusBadRequest)
					writePublisherJSON(t, w, metav1.Status{Reason: "invalid_json", Code: http.StatusBadRequest})
					return
				}
				if !request.Snapshot.Containers[0].Context.Resources.IsZero() {
					t.Error("rollback retained unsupported fields")
				}
				writePublisherJSON(t, w, validPublisherResponse(false))
			}))
			defer server.Close()
			publisher, err := newSnapshotPublisher(server.Client(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			err = publisher.Publish(t.Context(), "node-a", schemaSnapshot())
			if rollback {
				if err != nil || gets != 2 || posts != 2 {
					t.Fatalf("rollback: %v gets=%d posts=%d", err, gets, posts)
				}
			} else if err == nil || gets != 2 || posts != 1 {
				t.Fatalf("unchanged contract retried invalid data: %v gets=%d posts=%d", err, gets, posts)
			}
		})
	}
}

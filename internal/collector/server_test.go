package collector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/metrics"
)

func TestSnapshotEndpointStoresVersionedSnapshot(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	handler := NewHandler(store, time.Minute, func(string, ...any) {})

	rec := postSnapshot(t, handler, api.AgentSnapshot{
		SchemaVersion: api.CurrentSnapshotSchemaVersion,
		NodeName:      "node-a",
		CapturedAt:    now,
		Containers: []api.ContainerSnapshot{
			container("default", "api", "app", "id-a", now, 100),
		},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := store.ListContainers(now, time.Minute); len(got) != 1 {
		t.Fatalf("stored containers = %d, want 1", len(got))
	}
}

func TestReadAndIngestHandlersExposeSeparateSurfaces(t *testing.T) {
	store := NewStore()
	opts := DefaultHandlerOptions(time.Minute)
	readHandler := NewReadHandlerWithOptions(store, opts)
	ingestHandler := NewIngestHandlerWithOptions(store, opts, func(string, ...any) {})

	readSnapshot := httptest.NewRecorder()
	readHandler.ServeHTTP(readSnapshot, httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", nil))
	if readSnapshot.Code != http.StatusNotFound {
		t.Fatalf("read listener snapshot status = %d, want 404", readSnapshot.Code)
	}

	ingestMetrics := httptest.NewRecorder()
	ingestHandler.ServeHTTP(ingestMetrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if ingestMetrics.Code != http.StatusNotFound {
		t.Fatalf("ingest listener metrics status = %d, want 404", ingestMetrics.Code)
	}

	ingestReads := httptest.NewRecorder()
	ingestHandler.ServeHTTP(ingestReads, httptest.NewRequest(http.MethodGet, "/api/v1/pods", nil))
	if ingestReads.Code != http.StatusNotFound {
		t.Fatalf("ingest listener pods status = %d, want 404", ingestReads.Code)
	}
}

func TestPodHistoryEndpointReturnsBoundedPoints(t *testing.T) {
	now := time.Now().UTC()
	store := NewStoreWithHistory(HistoryOptions{Duration: time.Minute, MaxSeries: 10, MaxPoints: 2, MaxResponseSeries: 2})
	for i := 0; i < 3; i++ {
		_, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
			NodeName:   "node-a",
			CapturedAt: now.Add(time.Duration(i) * time.Second),
			Containers: []api.ContainerSnapshot{container("default", "api", "app", "id-a", now, uint64(100+i))},
		})
		if err != nil {
			t.Fatalf("ReplaceNodeSnapshot returned error: %v", err)
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/history/pods/default/api", nil)
	NewReadHandlerWithOptions(store, DefaultHandlerOptions(time.Minute)).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var history []api.PodHistory
	if err := json.Unmarshal(recorder.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 || len(history[0].Points) != 2 {
		t.Fatalf("unexpected history: %#v", history)
	}
}

func TestPodHistoryEndpointRejectsMalformedPath(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/history/pods/default/", nil)
	NewReadHandlerWithOptions(NewStore(), DefaultHandlerOptions(time.Minute)).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestReadEndpointRejectsResponseAboveByteLimit(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		container("default", "api", "application", "container-id", now, 100),
	}})
	opts := DefaultHandlerOptions(time.Minute)
	opts.MaxResponseBytes = 16
	recorder := httptest.NewRecorder()
	NewReadHandlerWithOptions(store, opts).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/containers", nil))
	if recorder.Code != http.StatusInsufficientStorage || !strings.Contains(recorder.Body.String(), "configured maximum") {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSnapshotEndpointReportsStoreCapacity(t *testing.T) {
	now := time.Now().UTC()
	store := NewStoreWithHistoryAndLimits(DefaultHistoryOptions(), StoreLimits{MaxNodes: 1, MaxContainers: 1})
	handler := NewHandler(store, time.Minute, func(string, ...any) {})
	first := api.AgentSnapshot{SchemaVersion: api.CurrentSnapshotSchemaVersion, NodeName: "node-a", CapturedAt: now}
	if recorder := postSnapshot(t, handler, first); recorder.Code != http.StatusOK {
		t.Fatalf("first snapshot status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	second := api.AgentSnapshot{SchemaVersion: api.CurrentSnapshotSchemaVersion, NodeName: "node-b", CapturedAt: now}
	recorder := postSnapshot(t, handler, second)
	if recorder.Code != http.StatusInsufficientStorage || !strings.Contains(recorder.Body.String(), "storage capacity") {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCollectorMetricsIncludeIngestionOutcomes(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	opts := DefaultHandlerOptions(time.Minute)
	ingestHandler := NewIngestHandlerWithOptions(store, opts, func(string, ...any) {})
	readHandler := NewReadHandlerWithOptions(store, opts)
	valid := api.AgentSnapshot{
		SchemaVersion: api.CurrentSnapshotSchemaVersion,
		NodeName:      "node-a",
		CapturedAt:    now,
	}
	if rec := postSnapshot(t, ingestHandler, valid); rec.Code != http.StatusOK {
		t.Fatalf("valid snapshot status = %d, want 200", rec.Code)
	}
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", strings.NewReader("{}"))
	invalidRec := httptest.NewRecorder()
	ingestHandler.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("invalid snapshot status = %d, want 415", invalidRec.Code)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	readHandler.ServeHTTP(metricsRec, metricsReq)
	for _, want := range []string{
		`kubememlens_collector_ingestion_requests_total{result="accepted"} 1`,
		`kubememlens_collector_ingestion_requests_total{result="unsupported_media_type"} 1`,
		"kubememlens_collector_ingestion_last_duration_seconds",
	} {
		if !strings.Contains(metricsRec.Body.String(), want) {
			t.Fatalf("metrics missing %q:\n%s", want, metricsRec.Body.String())
		}
	}
}

func TestSnapshotEndpointRejectsUnsupportedSchema(t *testing.T) {
	now := time.Now().UTC()
	rec := postSnapshot(t, NewHandler(NewStore(), time.Minute, func(string, ...any) {}), api.AgentSnapshot{
		SchemaVersion: api.CurrentSnapshotSchemaVersion + 1,
		NodeName:      "node-a",
		CapturedAt:    now,
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unsupported schemaVersion") {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
}

func TestValidateSnapshotBoundsKubernetesIdentityFields(t *testing.T) {
	now := time.Now().UTC()
	base := api.AgentSnapshot{
		SchemaVersion: api.CurrentSnapshotSchemaVersion, NodeName: "node-a", CapturedAt: now,
		Containers: []api.ContainerSnapshot{{ContainerID: "id-a"}},
	}
	tests := map[string]struct {
		mutate func(*api.ContainerSnapshot)
		field  string
	}{
		"namespace":      {mutate: func(item *api.ContainerSnapshot) { item.Namespace = strings.Repeat("n", 64) }, field: "namespace"},
		"Pod name":       {mutate: func(item *api.ContainerSnapshot) { item.PodName = strings.Repeat("p", 254) }, field: "podName"},
		"Pod UID":        {mutate: func(item *api.ContainerSnapshot) { item.PodUID = strings.Repeat("u", 129) }, field: "podUID"},
		"container name": {mutate: func(item *api.ContainerSnapshot) { item.ContainerName = strings.Repeat("c", 64) }, field: "containerName"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := base
			snapshot.Containers = append([]api.ContainerSnapshot(nil), base.Containers...)
			test.mutate(&snapshot.Containers[0])
			err := ValidateSnapshot(snapshot, now, DefaultHandlerOptions(time.Minute))
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("ValidateSnapshot error = %v", err)
			}
		})
	}
}

func TestSnapshotEndpointRejectsOutOfOrderSnapshot(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	handler := NewHandler(store, time.Minute, func(string, ...any) {})
	newer := api.AgentSnapshot{
		SchemaVersion: api.CurrentSnapshotSchemaVersion,
		NodeName:      "node-a",
		CapturedAt:    now,
	}
	if rec := postSnapshot(t, handler, newer); rec.Code != http.StatusOK {
		t.Fatalf("newer status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	older := newer
	older.CapturedAt = now.Add(-time.Second)

	rec := postSnapshot(t, handler, older)

	if rec.Code != http.StatusConflict {
		t.Fatalf("older status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

func TestSnapshotEndpointRejectsOversizedPayload(t *testing.T) {
	handler := NewHandlerWithOptions(NewStore(), HandlerOptions{
		SnapshotTTL:      time.Minute,
		MaxSnapshotBytes: 64,
	}, func(string, ...any) {})
	body := bytes.NewBufferString(`{"schemaVersion":1,"nodeName":"node-a","capturedAt":"2026-07-18T00:00:00Z","containers":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body.String())
	}
}

func TestSnapshotEndpointRejectsTrailingJSON(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := bytes.NewBufferString(`{"schemaVersion":1,"nodeName":"node-a","capturedAt":"` + now + `","containers":[]} {}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	NewHandler(NewStore(), time.Minute, func(string, ...any) {}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestSnapshotEndpointRejectsInvalidTimestampAndContainerCount(t *testing.T) {
	now := time.Now().UTC()
	tests := map[string]api.AgentSnapshot{
		"stale timestamp": {
			SchemaVersion: api.CurrentSnapshotSchemaVersion,
			NodeName:      "node-a",
			CapturedAt:    now.Add(-3 * time.Minute),
		},
		"future timestamp": {
			SchemaVersion: api.CurrentSnapshotSchemaVersion,
			NodeName:      "node-a",
			CapturedAt:    now.Add(time.Minute),
		},
		"too many containers": {
			SchemaVersion: api.CurrentSnapshotSchemaVersion,
			NodeName:      "node-a",
			CapturedAt:    now,
			Containers: []api.ContainerSnapshot{
				container("default", "a", "app", "id-a", now, 100),
				container("default", "b", "app", "id-b", now, 100),
			},
		},
	}

	for name, snapshot := range tests {
		t.Run(name, func(t *testing.T) {
			handler := NewHandlerWithOptions(NewStore(), HandlerOptions{
				SnapshotTTL:   time.Minute,
				MaxContainers: 1,
			}, func(string, ...any) {})
			rec := postSnapshot(t, handler, snapshot)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSnapshotEndpointRejectsInvalidContainerContext(t *testing.T) {
	now := time.Now().UTC()
	snapshot := api.AgentSnapshot{
		SchemaVersion: api.CurrentSnapshotSchemaVersion,
		NodeName:      "node-a",
		CapturedAt:    now,
		Containers: []api.ContainerSnapshot{
			container("default", "api", "app", "id-a", now, 100),
		},
	}
	snapshot.Containers[0].Context.RestartCount = -1
	rec := postSnapshot(t, NewHandler(NewStore(), time.Minute, func(string, ...any) {}), snapshot)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "restartCount") {
		t.Fatalf("unexpected response: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSnapshotEndpointRejectsExcessiveLabels(t *testing.T) {
	now := time.Now().UTC()
	snapshot := api.AgentSnapshot{
		SchemaVersion: api.CurrentSnapshotSchemaVersion,
		NodeName:      "node-a",
		CapturedAt:    now,
		Containers:    []api.ContainerSnapshot{container("default", "api", "app", "id-a", now, 100)},
	}
	snapshot.Containers[0].Context.Labels = map[string]string{}
	for i := 0; i < 65; i++ {
		snapshot.Containers[0].Context.Labels[fmt.Sprintf("label-%d", i)] = "value"
	}
	rec := postSnapshot(t, NewHandler(NewStore(), time.Minute, func(string, ...any) {}), snapshot)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "labels exceeds") {
		t.Fatalf("unexpected response: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetricsEndpointReturnsMetrics(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		container("default", "api", "app", "id-a", now, 100),
	}})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	NewHandlerWithOptions(store, HandlerOptions{SnapshotTTL: time.Minute, Metrics: metrics.DefaultOptions()}, func(string, ...any) {}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != metrics.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "kubememlens_namespace_memory_bytes") {
		t.Fatalf("metrics body missing namespace metric:\n%s", rec.Body.String())
	}
}

func TestMetricsEndpointWrongMethodReturns405(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	rec := httptest.NewRecorder()
	NewHandler(NewStore(), time.Minute, func(string, ...any) {}).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestMetricsEndpointDisabledReturns404(t *testing.T) {
	opts := metrics.DefaultOptions()
	opts.Enabled = false

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	NewHandlerWithOptions(NewStore(), HandlerOptions{SnapshotTTL: time.Minute, Metrics: opts}, func(string, ...any) {}).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func postSnapshot(t *testing.T, handler http.Handler, snapshot api.AgentSnapshot) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

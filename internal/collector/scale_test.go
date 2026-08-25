package collector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestContainerPagesServeTenThousandRealisticRecordsWithinResponseBound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 10,000-record API regression in short mode")
	}
	now := time.Now().UTC()
	containers := make([]api.ContainerSnapshot, 10_000)
	for index := range containers {
		containers[index] = scaleContainer(index, now)
	}
	store := NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "linux-pool-a", CapturedAt: now, Containers: containers,
	}); err != nil {
		t.Fatalf("store scale snapshot: %v", err)
	}
	opts := DefaultHandlerOptions(time.Minute)
	handler := NewReadHandlerWithOptions(store, opts)

	legacy := httptest.NewRecorder()
	handler.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/api/v1/containers", nil))
	if legacy.Code != http.StatusOK && legacy.Code != http.StatusInsufficientStorage {
		t.Fatalf("legacy endpoint status = %d, want 200 or bounded 507", legacy.Code)
	}
	t.Logf("legacy containers response: status=%d bytes=%d", legacy.Code, legacy.Body.Len())

	continuation := ""
	total := 0
	seen := map[string]struct{}{}
	for pageNumber := 1; ; pageNumber++ {
		path := fmt.Sprintf("/api/v1/pages/containers?limit=%d", maxContainerPageSize)
		if continuation != "" {
			path += "&continue=" + url.QueryEscape(continuation)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("page %d status=%d body=%s", pageNumber, recorder.Code, recorder.Body.String())
		}
		if recorder.Body.Len() > opts.MaxResponseBytes {
			t.Fatalf("page %d bytes=%d exceeds %d", pageNumber, recorder.Body.Len(), opts.MaxResponseBytes)
		}
		var page api.ContainerPage
		if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode page %d: %v", pageNumber, err)
		}
		if len(page.Items) == 0 || len(page.Items) > maxContainerPageSize {
			t.Fatalf("page %d contains %d items", pageNumber, len(page.Items))
		}
		for _, item := range page.Items {
			if _, duplicate := seen[item.ContainerID]; duplicate {
				t.Fatalf("duplicate container %q on page %d", item.ContainerID, pageNumber)
			}
			seen[item.ContainerID] = struct{}{}
		}
		total += len(page.Items)
		if page.Continue == "" {
			break
		}
		if page.Continue == continuation {
			t.Fatalf("page %d repeated its continuation token", pageNumber)
		}
		continuation = page.Continue
	}
	if total != len(containers) {
		t.Fatalf("paged containers=%d, want %d", total, len(containers))
	}
}

func TestContainerPageRejectsUntrustedPaginationInput(t *testing.T) {
	handler := NewReadHandlerWithOptions(NewStore(), DefaultHandlerOptions(time.Minute))
	for _, path := range []string{
		"/api/v1/pages/containers?limit=0",
		"/api/v1/pages/containers?limit=501",
		"/api/v1/pages/containers?limit=not-a-number",
		"/api/v1/pages/containers?continue=not-base64",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d, want 400", path, recorder.Code)
		}
	}
}

func TestContainerPageRejectsPreviousCollectorGeneration(t *testing.T) {
	now := time.Now().UTC()
	newHandler := func(generation string) http.Handler {
		store := NewStore()
		store.generation = generation
		_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
			scaleContainer(0, now), scaleContainer(1, now),
		}})
		return NewReadHandlerWithOptions(store, DefaultHandlerOptions(time.Minute))
	}
	first := httptest.NewRecorder()
	newHandler("first").ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/pages/containers?limit=1", nil))
	var page api.ContainerPage
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil || page.Continue == "" {
		t.Fatalf("first page token: page=%#v error=%v", page, err)
	}

	second := httptest.NewRecorder()
	newHandler("second").ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/v1/pages/containers?continue="+url.QueryEscape(page.Continue), nil))
	if second.Code != http.StatusBadRequest {
		t.Fatalf("previous-generation token status=%d body=%s", second.Code, second.Body.String())
	}
}

func scaleContainer(index int, capturedAt time.Time) api.ContainerSnapshot {
	pod := fmt.Sprintf("memory-worker-%05d", index)
	return api.ContainerSnapshot{
		Namespace: "memlens-density", PodName: pod, PodUID: "uid-" + pod,
		ContainerName: "worker", ContainerID: fmt.Sprintf("containerd://sha256:%064x", index+1),
		NodeName: "linux-pool-a", CgroupPath: "/sys/fs/cgroup/kubepods.slice/" + pod,
		CapturedAt: capturedAt,
		Context: api.ContainerContext{
			MemoryRequestBytes: 1 << 20, MemoryRequestKnown: true,
			MemoryLimitBytes: 8 << 20, MemoryLimitKnown: true,
			QoSClass: "Burstable", PodPhase: "Running", OwnerKind: "ReplicaSet",
			OwnerName: "memory-workers-7f6d9", WorkloadKind: "Deployment", WorkloadName: "memory-workers",
			Labels: map[string]string{"app.kubernetes.io/name": "memlens-density"},
		},
		Memory: model.MemoryBreakdown{
			Name: "memlens-density/" + pod + "/worker", TotalBytes: 4 << 20,
			AnonBytes: 2 << 20, FileBytes: 1 << 20, KernelBytes: 256 << 10,
			PeakKnown: true, PeakBytes: 5 << 20, MaxKnown: true, MaxBytes: 8 << 20,
			PressureKnown: true, PSISomeAvg10: 0.01, ReclaimCountersKnown: true,
		},
	}
}

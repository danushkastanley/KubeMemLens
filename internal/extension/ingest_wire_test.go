package extension

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// One megabyte of empty container objects decodes to several hundred
// megabytes of typed cgroup records, so the bound must apply to the wire form.
const (
	amplifyingBodyBytes  = 1 << 20
	maxIngestAllocations = 32 << 20
)

func TestHandlerBoundsCgroupContainersBeforeTypedAllocation(t *testing.T) {
	handler, _, _ := testHandler(t, 4<<20, 10, 2)
	body := `{"snapshot":{"containers":[` + strings.Repeat(`{},`, amplifyingBodyBytes/3) + `{}]}}`

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	recorder := serveRawSnapshot(handler, testClaims("pod-a", "node-a", "node-uid-a"), body)
	runtime.ReadMemStats(&after)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "containers exceeds maximum of 10000") {
		t.Fatalf("status = %d, want 400 container limit: %s", recorder.Code, recorder.Body.String())
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > maxIngestAllocations {
		t.Fatalf("rejected snapshot allocated %d bytes, want at most %d", allocated, maxIngestAllocations)
	}
}

func TestHandlerRejectsMalformedCgroupWireAsInvalidJSON(t *testing.T) {
	handler, _, _ := testHandler(t, 1<<20, 10, 4)
	claims := testClaims("pod-a", "node-a", "node-uid-a")
	for name, body := range map[string]string{
		"truncated":   `{"snapshot":{"containers":[{}`,
		"nested deep": strings.Repeat(`{"a":`, 80) + `1` + strings.Repeat(`}`, 80),
	} {
		t.Run(name, func(t *testing.T) {
			recorder := serveRawSnapshot(handler, claims, body)
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid_json") {
				t.Fatalf("status = %d, want 400 invalid_json: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func serveRawSnapshot(handler http.Handler, claims AgentClaims, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/apis/memory.kubememlens.io/v1alpha1/nodesnapshots", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, withClaims(request, claims))
	return recorder
}

package collector

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// One megabyte of empty container objects decodes to several hundred
// megabytes of typed cgroup records, so the bound must apply to the wire form.
const (
	amplifyingBodyBytes  = 1 << 20
	maxIngestAllocations = 32 << 20
)

func TestSnapshotEndpointBoundsContainersBeforeTypedAllocation(t *testing.T) {
	handler := NewHandler(NewStore(), time.Minute, func(string, ...any) {})
	body := `{"containers":[` + strings.Repeat(`{},`, amplifyingBodyBytes/3) + `{}]}`

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	recorder := postRawSnapshot(handler, body)
	runtime.ReadMemStats(&after)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "containers exceeds maximum of 10000") {
		t.Fatalf("status = %d, want 400 container limit: %s", recorder.Code, recorder.Body.String())
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > maxIngestAllocations {
		t.Fatalf("rejected snapshot allocated %d bytes, want at most %d", allocated, maxIngestAllocations)
	}
}

func TestSnapshotEndpointKeepsOversizedAndTrailingBodyResponses(t *testing.T) {
	handler := NewHandlerWithOptions(NewStore(), HandlerOptions{MaxSnapshotBytes: 64}, func(string, ...any) {})
	for name, test := range map[string]struct {
		body   string
		status int
		want   string
	}{
		"oversized": {body: `{"nodeName":"` + strings.Repeat("n", 64) + `"}`, status: http.StatusRequestEntityTooLarge, want: "maximum request size"},
		"trailing":  {body: `{"schemaVersion":1} {}`, status: http.StatusBadRequest, want: "invalid snapshot JSON"},
		"truncated": {body: `{"containers":[{}`, status: http.StatusBadRequest, want: "invalid snapshot JSON"},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := postRawSnapshot(handler, test.body)
			if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), test.want) {
				t.Fatalf("status = %d, want %d %q: %s", recorder.Code, test.status, test.want, recorder.Body.String())
			}
		})
	}
}

func postRawSnapshot(handler http.Handler, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

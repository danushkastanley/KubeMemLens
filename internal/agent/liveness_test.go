package agent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func probe(handler http.Handler, method, path string) int {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder.Code
}

func TestLivenessFailsOnlyWhenTheScanLoopStops(t *testing.T) {
	started := time.Unix(1_000, 0)
	now := started
	telemetry := &Telemetry{}
	handler := telemetry.LivenessHandler(started, 2*time.Minute, func() time.Time { return now })

	if code := probe(handler, "GET", "/livez"); code != 200 {
		t.Fatalf("a freshly started agent is not live: %d", code)
	}
	now = started.Add(2*time.Minute + time.Second)
	if code := probe(handler, "GET", "/livez"); code != 503 {
		t.Fatalf("an agent that never scanned is still live after the window: %d", code)
	}
	// A failed scan is still an attempt: the loop is running.
	telemetry.RecordScan(now, time.Second, ScanResult{}, errors.New("scan failed"), 0)
	if code := probe(handler, "GET", "/livez"); code != 200 {
		t.Fatalf("an agent that has just attempted a scan is not live: %d", code)
	}
	now = now.Add(2*time.Minute + time.Second)
	if code := probe(handler, "GET", "/livez"); code != 503 {
		t.Fatalf("an agent whose scans stopped is still live: %d", code)
	}
	if code := probe(handler, "GET", "/healthz"); code != 200 {
		t.Fatalf("/healthz depends on scanning: %d", code)
	}
}

func TestLivenessListenerServesNothingElse(t *testing.T) {
	handler := (&Telemetry{}).LivenessHandler(time.Now(), time.Minute, time.Now)
	for _, path := range []string{"/metrics", "/scan-observations", "/"} {
		if code := probe(handler, "GET", path); code != 404 {
			t.Fatalf("%s on the health listener: %d", path, code)
		}
	}
	if code := probe(handler, "POST", "/livez"); code != 405 {
		t.Fatalf("POST /livez: %d", code)
	}
}

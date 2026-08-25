package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandlerDoesNotExposeCollectorData(t *testing.T) {
	handler := NewHealthHandler()
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}

	for _, path := range []string{"/api/v1/pods", "/api/v1/debug/store", "/metrics"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, recorder.Code)
		}
	}
}

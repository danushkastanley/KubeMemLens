package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandlerDoesNotExposeCollectorData(t *testing.T) {
	handler := NewHealthHandler()
	for _, path := range []string{"/livez", "/healthz"} {
		health := httptest.NewRecorder()
		handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, path, nil))
		if health.Code != http.StatusOK || health.Body.String() != "ok\n" {
			t.Fatalf("%s status=%d body=%q", path, health.Code, health.Body.String())
		}
	}

	for _, path := range []string{"/readyz", "/api/v1/pods", "/api/v1/debug/store", "/metrics"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, recorder.Code)
		}
	}
}

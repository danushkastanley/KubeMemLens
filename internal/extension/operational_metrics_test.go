package extension

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
)

func TestAuthenticatedMetricsExposeActualHeapWithoutVolumeIdentities(t *testing.T) {
	h := NewReadHandler(collector.NewStore(), collector.DefaultHandlerOptions(time.Minute))
	path := "/apis/memory.kubememlens.io/v1alpha1/metrics/current"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, readRequest(t, path, true))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var value api.Metrics
	if json.Unmarshal(w.Body.Bytes(), &value) != nil {
		t.Fatal("invalid metrics envelope")
	}
	matches := regexp.MustCompile(`(?m)^kubememlens_collector_heap_objects_bytes ([0-9]+)$`).FindStringSubmatch(value.Content)
	if len(matches) != 2 {
		t.Fatal("runtime heap metric missing")
	}
	heap, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil || heap == 0 {
		t.Fatal("heap not measured")
	}
	if !strings.Contains(value.Content, "kubememlens_volume_health_enabled 0\n") || !strings.Contains(value.Content, "kubememlens_volume_usage_entries 0\n") {
		t.Fatal("disabled store configuration not explicit")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, readRequest(t, path, false))
	if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "heap_objects") {
		t.Fatal("unauthenticated metrics disclosure")
	}
}

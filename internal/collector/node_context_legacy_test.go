package collector

import (
	"net/http"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestLegacyIngestionCannotAcceptNodeContext(t *testing.T) {
	now := time.Now().UTC()
	value := contextSample(now)
	store := NewStore()
	handler := NewIngestHandlerWithOptions(store, DefaultHandlerOptions(time.Minute), func(string, ...any) {})
	response := postSnapshot(t, handler, api.AgentSnapshot{SchemaVersion: 3, NodeName: value.NodeName, CapturedAt: now, NodeContext: &value})
	if response.Code != http.StatusBadRequest || store.NodeContextDebug(now).Records != 0 {
		t.Fatalf("legacy Node ingestion: %d %s", response.Code, response.Body.String())
	}
}

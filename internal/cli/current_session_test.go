package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestAutoExplainPreservesGetOnlyDeepPodAccess(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1":
			_, _ = w.Write([]byte(`{}`))
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/app":
			_ = json.NewEncoder(w).Encode(api.PodMemory{Snapshot: api.PodSnapshot{Namespace: "team-a", PodName: "app", Memory: model.MemoryBreakdown{TotalBytes: 32 << 20}}})
		default:
			w.WriteHeader(403)
		}
	}))
	defer server.Close()
	out, err := runRestrictedCLI(t, kubeconfigForTLS(t, server), "auto", "explain", "pod", "app", "-n", "team-a", "-o", "json")
	if err != nil {
		t.Fatalf("get-only deep caller lost access: %v", err)
	}
	var document explanationDocument
	if err := json.Unmarshal([]byte(out), &document); err != nil || document.Memory.TotalBytes != 32<<20 || document.SchemaVersion != api.CurrentExplanationSchemaVersion {
		t.Fatalf("deep contract changed: %s %v", out, err)
	}
}

func TestAutoScopeDenialRetainsPermissionDiagnostic(t *testing.T) {
	var status atomic.Int32
	_, err := runRestrictedCLI(t, restrictedConfig(t, &status), "auto", "top", "pods", "-A")
	if err == nil || (!strings.Contains(strings.ToLower(err.Error()), "permission") && !strings.Contains(strings.ToLower(err.Error()), "forbidden")) {
		t.Fatalf("denial no longer identifies a permission failure: %v", err)
	}
}

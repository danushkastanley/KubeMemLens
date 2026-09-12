package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func TestMissingDeepPodDoesNotBecomeAnAbsentAPI(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/tenant-a/pods/missing":
			w.WriteHeader(404)
		case "/apis/memory.kubememlens.io/v1alpha1":
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected fallback: %s", r.URL.Path)
			w.WriteHeader(403)
		}
	}))
	defer server.Close()
	session, err := NewPodEvidenceSession(t.Context(), evidenceOptions(t, server), "missing")
	if err != nil || session.Plan.Mode != capability.Deep || len(paths) != 2 {
		t.Fatalf("session=%+v err=%v paths=%v", session, err, paths)
	}
	if session.Plan.Sources[0].Reason != capability.NotObserved {
		t.Fatal("missing target was reported as source absence")
	}
}

func TestSelectedPodProbeRejectsAmbiguousOrUnsafeTargetsBeforeIO(t *testing.T) {
	for _, test := range []struct {
		scope ReadScope
		name  string
	}{{AllNamespacesScope(), "app"}, {ReadScope{}, "../app"}, {ReadScope{}, ""}} {
		if _, err := NewPodEvidenceSession(t.Context(), Options{ReadScope: test.scope}, test.name); err == nil {
			t.Fatal("invalid target accepted")
		}
	}
}

func TestSelectionPermissionClassificationMatchesLegacy401And403(t *testing.T) {
	for _, reason := range []capability.Reason{capability.AccessDenied, capability.AuthenticationFailed, capability.RequestFailed} {
		want := reason != capability.RequestFailed
		if got := IsForbidden(fmt.Errorf("wrapped: %w", &capability.SelectionError{Mode: capability.Restricted, Reason: reason})); got != want {
			t.Fatalf("reason=%s forbidden=%v", reason, got)
		}
	}
}

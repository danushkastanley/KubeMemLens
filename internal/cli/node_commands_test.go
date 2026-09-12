package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
)

func TestNodeCommandsUseOnlyAuthorisedNodeResources(t *testing.T) {
	calls := 0
	denied := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get(api.SnapshotSchemaHeader) != "3" {
			t.Error("Node command did not negotiate schema 3")
		}
		if denied {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var value any
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/node-a":
			value = api.NodeContextResource{Record: api.NodeContextRecord{NodeName: "node-a", NodeUID: "node-uid"}}
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/node-a/analysis":
			if r.URL.Query().Get("rank") != "psi" || r.URL.Query().Get("limit") != "3" {
				t.Error("ranking arguments lost")
			}
			value = api.NodeMemoryAnalysis{Analysis: nodeanalysis.Analysis{SchemaVersion: 1, NodeName: "node-a", NodeUID: "node-uid", ContributorAccess: nodeanalysis.NodeOnly, Facts: nodeanalysis.Facts{Availability: capability.Unreported}}}
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/node-a/history":
			value = api.NodeContextHistory{NodeName: "node-a", Generation: "generation", Completeness: capability.Partial}
		default:
			t.Error("Node-only command requested unrelated resource", r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	config := kubeconfigForTLS(t, server)
	for _, format := range []string{"text", "json", "yaml"} {
		out, err := runRestrictedCLI(t, config, "deep", "explain", "node", "node-a", "--rank", "psi", "--limit", "3", "-o", format)
		if err != nil || !strings.Contains(out, "node-a") || !strings.Contains(out, "node-only") {
			t.Fatal(format, out, err)
		}
		out, err = runRestrictedCLI(t, config, "deep", "history", "node", "node-a", "-o", format)
		if err != nil || !strings.Contains(out, "node-a") {
			t.Fatal(format, out, err)
		}
	}
	if calls != 9 {
		t.Fatal("unexpected request count", calls)
	}
	denied = true
	out, err := runRestrictedCLI(t, config, "deep", "explain", "node", "node-a", "--rank", "psi", "--limit", "3")
	if err == nil || strings.Contains(out, "Node: node-a") {
		t.Fatal("denied command returned evidence", out, err)
	}
}

func TestNodeCommandsRejectInvalidModesAndArgumentsBeforeRead(t *testing.T) {
	for _, args := range [][]string{
		{"explain", "node", "node-a", "--rank", "invalid"},
		{"explain", "node", "node-a", "--limit", "101"},
		{"explain", "node", "node-a", "-o", "table"},
		{"history", "node", "node-a", "--since", "1h"},
		{"history", "node", "node-a", "--since=-1s"},
	} {
		out, err := runRestrictedCLI(t, "/nonexistent-config", "deep", args...)
		if err == nil || strings.Contains(out, "no such file") {
			t.Fatal("invalid options reached connection", args, out, err)
		}
	}
	_, err := runRestrictedCLI(t, "/nonexistent-config", "restricted", "explain", "node", "node-a")
	if err == nil {
		t.Fatal("restricted mode produced deep Node analysis")
	}
}

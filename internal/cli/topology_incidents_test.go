package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTopologyCLIExplicitCaptureReplayAndRevocation(t *testing.T) {
	now := time.Now().UTC().Add(-time.Second)
	usage := uint64(1234)
	o := nodecontext.Observation{NodeName: "worker-a", NodeUID: "private-uid", ReportedAt: now, Availability: capability.Available, Evidence: capability.Envelope{Source: nodecontext.Source, APIVersion: "v1alpha1", Scope: capability.NodeScope, Stability: capability.ImplementationSpecific, Completeness: capability.Partial, Freshness: capability.Fresh, ReceivedAt: now, CapturedAt: now}, Stats: &nodecontext.Stats{StartedAt: now.Add(-time.Hour), Provenance: nodecontext.Unknown, Memory: &nodecontext.Memory{CapturedAt: now, UsageBytes: &usage}}}
	analysis, err := nodeanalysis.Analyse(nodeanalysis.Input{Now: now, NodeName: o.NodeName, NodeUID: o.NodeUID, Current: &o, SourceAvailability: capability.Available, Access: nodeanalysis.NodeOnly})
	if err != nil {
		t.Fatal(err)
	}
	topology, err := memorytopology.Analyse(memorytopology.Failure(o.NodeName, o.NodeUID, now), now)
	if err != nil {
		t.Fatal(err)
	}
	var denied atomic.Bool
	var topologyCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.SnapshotSchemaHeader) != "7" || r.Header.Get("Authorization") != "Bearer fixture-reader" {
			t.Error("capture lost schema or credentials")
		}
		var value any
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/worker-a":
			value = api.NodeContextResource{Record: api.NodeContextRecord{NodeName: o.NodeName, NodeUID: o.NodeUID, ReceivedAt: now, Freshness: capability.Fresh, Report: &o, LastGood: &o}}
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/worker-a/analysis":
			value = api.NodeMemoryAnalysis{Analysis: analysis}
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/worker-a/topology":
			topologyCalls.Add(1)
			if denied.Load() {
				w.WriteHeader(403)
				return
			}
			value = api.NodeMemoryTopology{TypeMeta: metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "NodeMemoryTopology"}, ObjectMeta: metav1.ObjectMeta{Name: o.NodeName}, Current: &topology}
		default:
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	config := kubeconfigForTLS(t, server)
	path := filepath.Join(t.TempDir(), "topology.json")
	out, err := runRestrictedCLI(t, config, "deep", "capture", "--node", o.NodeName, "--include-topology", "-o", path)
	if err != nil {
		t.Fatal(out, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if topologyCalls.Load() != 1 || bytes.Contains(body, []byte("private-uid")) || bytes.Contains(body, []byte("worker-a")) {
		t.Fatal("capture scope or privacy changed")
	}
	doc, err := incident.Read(path)
	if err != nil || doc.Topology == nil {
		t.Fatal("invalid schema7 capture", err)
	}
	out, err = runRestrictedCLI(t, "/missing-offline-config", "deep", "replay", path)
	if err != nil || !strings.Contains(out, "NUMA") || !strings.Contains(out, "node-1") {
		t.Fatal("offline replay failed", out, err)
	}
	denied.Store(true)
	_, err = runRestrictedCLI(t, config, "deep", "capture", "--node", o.NodeName, "--include-topology", "--force", "-o", path)
	if err == nil {
		t.Fatal("revoked overwrite succeeded")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(body, after) {
		t.Fatal("denied overwrite changed file")
	}
	legacy := filepath.Join(t.TempDir(), "node.json")
	_, err = runRestrictedCLI(t, config, "deep", "capture", "--node", o.NodeName, "-o", legacy)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = incident.Read(legacy)
	if err != nil || doc.Node == nil || topologyCalls.Load() != 2 {
		t.Fatal("default capture queried topology or changed schema")
	}
}
func TestTopologyCaptureRejectsMixedScopes(t *testing.T) {
	for _, args := range [][]string{{"capture", "--include-topology"}, {"capture", "--schema-version", "7"}, {"capture", "--include-topology", "--node", "worker", "--schema-version", "4"}, {"capture", "--include-topology", "--node", "worker", "--volumes"}} {
		if _, err := runRestrictedCLI(t, "/missing-config", "deep", args...); err == nil {
			t.Fatal("invalid scope accepted", args)
		}
	}
}

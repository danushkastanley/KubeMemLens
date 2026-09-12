package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func TestNodeCLIAuthenticatedCaptureOfflineReplayCompareAndRevocation(t *testing.T) {
	now := time.Now().UTC().Add(-time.Second)
	usage := uint64(1234)
	o := nodecontext.Observation{NodeName: "worker-a", NodeUID: "private-uid", ReportedAt: now, Availability: capability.Available, Evidence: capability.Envelope{Source: nodecontext.Source, APIVersion: "v1alpha1", Scope: capability.NodeScope, Stability: capability.ImplementationSpecific, Completeness: capability.Partial, Freshness: capability.Fresh, ReceivedAt: now, CapturedAt: now}, Stats: &nodecontext.Stats{StartedAt: now.Add(-time.Hour), Provenance: nodecontext.Unknown, Memory: &nodecontext.Memory{CapturedAt: now, UsageBytes: &usage}}}
	record := api.NodeContextResource{Record: api.NodeContextRecord{NodeName: o.NodeName, NodeUID: o.NodeUID, ReceivedAt: now, Freshness: capability.Fresh, Report: &o, LastGood: &o}}
	history := api.NodeContextHistory{NodeName: o.NodeName, Generation: "private-generation", ResetAt: now.Add(-time.Hour), WindowSeconds: 900, Completeness: capability.Partial, Series: []api.NodeContextHistorySeries{{NodeUID: o.NodeUID, Points: []api.NodeContextHistoryPoint{{ReceivedAt: now, Observation: o}}}}}
	access := nodeanalysis.ClusterPods
	denyHistory, denyAnalysis := false, false
	var order []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.SnapshotSchemaHeader) != strconv.Itoa(api.CurrentSnapshotSchemaVersion) || r.Header.Get("Authorization") != "Bearer fixture-reader" {
			t.Error("capture lost schema or identity")
		}
		var value any
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/worker-a/history":
			order = append(order, "history")
			if denyHistory {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			value = history
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/worker-a":
			order = append(order, "record")
			value = record
		case "/apis/memory.kubememlens.io/v1alpha1/nodecontexts/worker-a/analysis":
			order = append(order, "analysis")
			if denyAnalysis {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			a, err := nodeanalysis.Analyse(nodeanalysis.Input{Now: now, NodeName: o.NodeName, NodeUID: o.NodeUID, Current: &o, SourceAvailability: capability.Available, Access: access, Cgroup: nodeanalysis.CgroupFrame{NodeUID: o.NodeUID, CapturedAt: now, Coverage: nodeanalysis.Complete, Containers: []nodeanalysis.Container{{ID: "container-id", Namespace: "private-team", PodName: "private-pod", PodUID: "private-pod-uid", ContainerName: "app", CompositionConsistent: true, Charge: nodeanalysis.Charges{Total: 100, Anon: 100}}}}})
			if err != nil {
				t.Error(err)
			}
			value = api.NodeMemoryAnalysis{Analysis: a}
		default:
			t.Error("unexpected capture resource", r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	config := kubeconfigForTLS(t, server)
	path := filepath.Join(t.TempDir(), "node.json")
	out, err := runRestrictedCLI(t, config, "deep", "capture", "--node", o.NodeName, "--include-history", "-o", path)
	if err != nil {
		t.Fatal(out, err)
	}
	if fmt.Sprint(order) != "[history record analysis]" {
		t.Fatal("capture did not authorise contributors last", order)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-uid", "private-generation", "private-team", "private-pod", "fixture-reader"} {
		if bytes.Contains(body, []byte(private)) {
			t.Fatal("capture leaked", private)
		}
	}
	for _, args := range [][]string{{"replay", path}, {"compare", "--before", path, "--after", path, "--node", o.NodeName}} {
		out, err := runRestrictedCLI(t, "/missing-offline-kubeconfig", "deep", args...)
		if err != nil || !strings.Contains(out, o.NodeName) {
			t.Fatal("offline operation failed", out, err)
		}
	}
	// A new command uses the new secondary decision, even for an explicitly
	// sensitive export. No previously authorised contributor is cached.
	access = nodeanalysis.NodeOnly
	order = nil
	out, err = runRestrictedCLI(t, config, "deep", "capture", "--node", o.NodeName, "--include-sensitive", "-o", "-")
	if err != nil || strings.Contains(out, "private-pod") || strings.Contains(out, "rankings") {
		t.Fatal("revocation leaked contributor data", out, err)
	}
	var bundle incident.NodeBundle
	if err := json.Unmarshal([]byte(out), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Evidence.Analysis.Coverage != nil || bundle.Evidence.Analysis.ObservedPodCharge != nil {
		t.Fatal("Node-only capture retained derived private fields")
	}
	denyAnalysis = true
	out, err = runRestrictedCLI(t, config, "deep", "capture", "--node", o.NodeName, "-o", "-")
	if err == nil || out != "" {
		t.Fatal("failed capture wrote partial stdout", out, err)
	}
	denyHistory = true
	_, err = runRestrictedCLI(t, config, "deep", "capture", "--node", o.NodeName, "--include-history", "--force", "-o", path)
	if err == nil {
		t.Fatal("denied history export succeeded")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(body, after) {
		t.Fatal("failed overwrite damaged previous capture")
	}
}

func TestNodeIncidentCLIRejectsIncompatibleSelections(t *testing.T) {
	for _, args := range [][]string{
		{"capture", "--node", "worker-a", "-n", "team"}, {"capture", "--node", "worker-a", "--schema-version", "2"}, {"capture", "--schema-version", "4"},
		{"compare", "--node", "worker-a", "pod-a", "pod-b"}, {"replay", "missing.json", "--node", "worker-a", "--pod", "team/app"},
	} {
		_, err := runRestrictedCLI(t, "/missing-config", "deep", args...)
		if err == nil {
			t.Fatal("incompatible selection accepted", args)
		}
	}
	deep := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(deep, []byte(`{"schemaVersion":1,"pods":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := runRestrictedCLI(t, "/missing-config", "deep", "replay", deep, "--node", "worker-a")
	if err == nil {
		t.Fatal("legacy incident accepted Node selector")
	}
}

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

func TestReplicasCLIUsesAuthenticatedPodAndWorkloadRoutes(t *testing.T) {
	var denied atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-reader" {
			t.Error("reader identity lost")
		}
		if denied.Load() {
			w.WriteHeader(403)
			return
		}
		now := time.Now().UTC()
		root := changemarkers.Object{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team-a", Name: "app", UID: "root-uid"}
		pod := changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: "app-0", UID: "pod-uid"}
		requested := pod
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/app-0/replicas":
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/workloads/app/replicas":
			requested = root
			if r.URL.Query().Get("kind") != "Deployment" {
				t.Error("workload kind lost")
			}
		default:
			t.Error("unexpected replica route")
			w.WriteHeader(404)
			return
		}
		report, err := replicabaseline.Analyse(replicabaseline.Input{Workload: root, ObservedAt: now, Peers: []replicabaseline.Peer{{Object: pod, WorkloadUID: root.UID, Revision: "rs", Shape: strings.Repeat("a", 64), Lifecycle: replicabaseline.Ready, Changes: replicabaseline.ChangeUnknown, SampleState: replicabaseline.Available, HistoryState: replicabaseline.Unreported, CapturedAt: now, StableSince: now.Add(-time.Hour), Values: map[replicabaseline.Metric]replicabaseline.Value{replicabaseline.Charge: {State: replicabaseline.Available, Number: 32 << 20}}}}})
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "memory.kubememlens.io/v1alpha1", "kind": "ReplicaBaseline", "metadata": map[string]string{"name": requested.Name, "namespace": requested.Namespace, "uid": requested.UID}, "requested": requested, "baseline": report})
	}))
	defer server.Close()
	config := kubeconfigForTLS(t, server)
	out, err := runRestrictedCLI(t, config, "deep", "replicas", "pod", "app-0", "-n", "team-a")
	if err != nil || !strings.Contains(out, "insufficient comparable peers") || !strings.Contains(out, "Informational only") {
		t.Fatal(out, err)
	}
	out, err = runRestrictedCLI(t, config, "deep", "replicas", "workload", "deployment/app", "-n", "team-a", "-o", "json")
	var report replicabaseline.Report
	if err != nil || json.Unmarshal([]byte(out), &report) != nil || report.Validate() != nil {
		t.Fatal(out, err)
	}
	denied.Store(true)
	out, err = runRestrictedCLI(t, config, "deep", "replicas", "pod", "app-0", "-n", "team-a")
	if err == nil || strings.Contains(out, "pod-uid") {
		t.Fatal("denied replica evidence disclosed")
	}
}

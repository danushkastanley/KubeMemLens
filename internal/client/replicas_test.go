package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

func TestReplicaClientValidatesScopeClockAndPeerEvidence(t *testing.T) {
	for _, change := range []string{"none", "foreign namespace", "replaced Pod", "future", "stale", "tampered metric", "private caveat"} {
		t.Run(change, func(t *testing.T) {
			now := time.Now().UTC()
			root := changemarkers.Object{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team-a", Name: "api", UID: "root"}
			pod := changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: "api-0", UID: "pod"}
			report, err := replicabaseline.Analyse(replicabaseline.Input{Workload: root, ObservedAt: now, Peers: []replicabaseline.Peer{{Object: pod, WorkloadUID: root.UID, Revision: "rs", Shape: strings.Repeat("a", 64), Lifecycle: replicabaseline.Ready, Changes: replicabaseline.ChangeUnknown, SampleState: replicabaseline.Available, HistoryState: replicabaseline.Unreported, CapturedAt: now, StableSince: now.Add(-time.Hour), Values: map[replicabaseline.Metric]replicabaseline.Value{replicabaseline.Charge: {State: replicabaseline.Available, Number: 100}}}}})
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "foreign namespace":
				report.Workload.Namespace = "other"
			case "replaced Pod":
				report.Peers[0].Peer.UID = "replacement"
			case "future":
				report.ObservedAt = now.Add(time.Hour)
			case "stale":
				report.ObservedAt = now.Add(-time.Minute)
			case "tampered metric":
				report.Peers[0].Comparisons[0].Outlier = "higher"
			case "private caveat":
				report.Caveats = append(report.Caveats, "\x1b[31m")
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api-0/replicas" || r.URL.RawQuery != "" {
					t.Error("wrong replica path")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"kind": "ReplicaBaseline", "apiVersion": "memory.kubememlens.io/v1alpha1", "metadata": map[string]string{"namespace": "team-a", "name": "api-0", "uid": "pod"}, "requested": pod, "baseline": report})
			}))
			defer server.Close()
			scope, _ := NamespaceScope("team-a")
			c := newTestKubernetesAPIClient(t, server.URL, scope)
			request := memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "team-a", Name: "api-0"}
			_, err = c.ReplicaBaseline(t.Context(), request)
			if (err == nil) != (change == "none") {
				t.Fatalf("validation: %v", err)
			}
			request.Namespace = "team-b"
			if _, err := c.ReplicaBaseline(t.Context(), request); err == nil || calls != 1 {
				t.Fatal("cross-tenant evidence acquired")
			}
		})
	}
}

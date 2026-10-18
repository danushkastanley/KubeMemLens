package main

import (
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	mh "github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/promhistory"
)

// Qualification depends on this source exercising the production decoder's
// real success/failure boundaries, including its accepted maximum response.
func TestFixtureExercisesProductionHistoryContract(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	selection := mh.Selection{Request: mh.Request{Scope: mh.Workload, Namespace: "fixture", Name: "deployment", WorkloadKind: "Deployment"}, UID: "deployment-uid", ResolvedAt: now}
	config := configuration{Mode: "normal"}
	for i := range mh.MaxTargets {
		pod, uid := fmt.Sprintf("pod-%02d", i), fmt.Sprintf("uid-%02d", i)
		id := fmt.Sprintf("containerd://instance-%02d", i)
		selection.Targets = append(selection.Targets, mh.Target{Namespace: "fixture", Pod: pod, PodUID: uid, Container: "app", ContainerID: id, Node: "node-a", NodeUID: "node-uid", StartedAt: now.Add(-25 * time.Hour), PodCreatedAt: now.Add(-26 * time.Hour)})
		config.Series = append(config.Series, series{Labels: map[string]string{"cluster": "fixture", "node": "node-a", "node_uid": "node-uid", "namespace": "fixture", "pod": pod, "pod_uid": uid, "container": "app", "container_id": id}, Started: now.Add(-25 * time.Hour).Unix(), WorkingSet: uint64(i) * 1048576, RSS: 524288})
	}
	path := filepath.Join(t.TempDir(), "config.json")
	f := &fixture{path: path, token: "fixture-only-token"}
	server := httptest.NewTLSServer(http.HandlerFunc(f.query))
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := promhistory.New(promhistory.Options{URL: server.URL, Cluster: "fixture", CAData: ca, BearerToken: f.token})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	query := mh.Query{Source: mh.Prometheus, Metric: mh.WorkingSet, Start: now.Add(-24 * time.Hour), End: now, Step: 6 * time.Minute}
	for _, mode := range []string{"normal", "gaps", "stale", "wrong-identity", "duplicate", "malformed", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			config.Mode = mode
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			report, err := client.Query(t.Context(), selection, query)
			switch mode {
			case "wrong-identity", "duplicate", "malformed":
				if !errors.Is(err, mh.ErrSource) || len(report.Series) != 0 {
					t.Fatalf("hostile fixture did not fail closed: %v", err)
				}
				return
			case "unavailable":
				if !errors.Is(err, mh.ErrUnavailable) || len(report.Series) != 0 {
					t.Fatalf("failure fixture returned evidence: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = report.Validate(); err != nil {
				t.Fatal(err)
			}
			if len(report.Series) != 16 || len(report.Series[0].Points) != 241 {
				t.Fatal("maximum profile was not exercised")
			}
			encoded, err := json.Marshal(report)
			if err != nil || len(encoded) > mh.MaxResponseBytes {
				t.Fatal("fixture exceeds API response budget")
			}
			switch mode {
			case "normal":
				point := report.Series[0].Points[0]
				if report.Completeness != mh.Complete || point.Bytes == nil || *point.Bytes != 0 {
					t.Fatal("normal fixture lost complete coverage or zero")
				}
			case "gaps":
				if report.Completeness != mh.Partial || report.Series[0].Points[1].Bytes != nil {
					t.Fatal("gap fixture invented a value")
				}
			case "stale":
				if report.State != mh.Stale || report.Series[0].Points[0].State != mh.Stale {
					t.Fatal("stale fixture reported fresh evidence")
				}
			}
		})
	}
	if f.requests.Load() != 7 || f.active.Load() != 0 {
		t.Fatal("fixture query accounting is inconsistent")
	}
}

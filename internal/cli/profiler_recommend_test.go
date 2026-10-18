package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/profiler"
)

func TestProfilerRecommendationsUseOnlyAuthorisedPodRead(t *testing.T) {
	var deny atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture-reader" {
			t.Error("unexpected method or identity")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1":
			_, _ = w.Write([]byte(`{}`))
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/app":
			if deny.Load() {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			now := time.Now().UTC()
			c := api.ContainerSnapshot{Namespace: "team-a", PodName: "app", PodUID: "uid", ContainerName: "app", ContainerID: "id", CapturedAt: now,
				Freshness: api.EvidenceFreshnessFresh, Completeness: api.EvidenceComplete, Memory: model.MemoryBreakdown{TotalBytes: 100 << 20, AnonBytes: 80 << 20},
				Context: api.ContainerContext{PodPhase: "Running", Labels: map[string]string{profiler.LabelPrefix + "app": profiler.GoHeapProfile, "private": "secret"}}}
			p := api.PodSnapshot{Namespace: c.Namespace, PodName: c.PodName, PodUID: c.PodUID, CapturedAt: now, Freshness: c.Freshness, Completeness: c.Completeness,
				Context: api.PodContext{Phase: "Running"}, Containers: []api.ContainerSnapshot{c}, Memory: c.Memory}
			_ = json.NewEncoder(w).Encode(api.PodMemory{Snapshot: p})
		default:
			t.Errorf("unexpected read: %s", r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer server.Close()
	config := kubeconfigForTLS(t, server)
	for _, format := range []string{"json", "yaml", "text"} {
		out, err := runRestrictedCLI(t, config, "deep", "recommend", "pod", "app", "-n", "team-a", "--profilers", "-o", format)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "go tool pprof -top") || strings.Contains(out, "secret") {
			t.Fatalf("invalid guidance: %s", out)
		}
		if format == "json" {
			var doc recommendationDocument
			if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.SchemaVersion != 4 || len(doc.ProfilerHandoffs) != 1 || doc.AutomaticMutation {
				t.Fatalf("invalid typed document: %s", out)
			}
		}
	}
	out, err := runRestrictedCLI(t, config, "deep", "recommend", "pod", "app", "-n", "team-a", "-o", "json")
	if err != nil || strings.Contains(out, "profilerHandoffs") || !strings.Contains(out, `"schemaVersion": 1`) {
		t.Fatalf("default contract changed: %s %v", out, err)
	}
	deny.Store(true)
	out, err = runRestrictedCLI(t, config, "deep", "recommend", "pod", "app", "-n", "team-a", "--profilers", "-o", "json")
	if err == nil || strings.Contains(out, "go tool pprof") {
		t.Fatal("denied read returned handoff")
	}
}

func TestProfilerFlagsRejectCombinedProfilesBeforeRead(t *testing.T) {
	for _, target := range [][]string{{"pod", "app"}, {"workload", "deployment/app"}} {
		args := append([]string{"recommend"}, target...)
		args = append(args, "--volumes", "--profilers")
		_, err := runRestrictedCLI(t, "/does-not-exist", "deep", args...)
		if err == nil || !strings.Contains(err.Error(), "profilers") || !strings.Contains(err.Error(), "volumes") {
			t.Fatalf("expected flag conflict: %v", err)
		}
	}
}

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHistoryTrendsCLIUsesAuthenticatedReaderAndExplicitSource(t *testing.T) {
	var deny atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-reader" {
			t.Error("reader identity lost")
		}
		if deny.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends" {
			t.Error("unexpected history path")
			http.NotFound(w, r)
			return
		}
		now := time.Now().UTC()
		q, err := memoryhistory.ParseQuery(r.URL.Query(), memoryhistory.Pod, now)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		s := memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "team-a", Name: "api"}, UID: "pod-uid", ResolvedAt: now, Targets: []memoryhistory.Target{{Namespace: "team-a", Pod: "api", PodUID: "pod-uid", Container: "app", ContainerID: "containerd://current", Node: "node-a", NodeUID: "node-uid", StartedAt: now.Add(-48 * time.Hour), PodCreatedAt: now.Add(-48 * time.Hour)}}}
		report, err := memoryhistory.NewReport(s, q, now)
		if err != nil {
			t.Error(err)
			return
		}
		series := &report.Series[0]
		series.Origin, series.SampleClock = "cadvisor", "prometheus-sample"
		if q.Source == memoryhistory.Local {
			series.Target.Container, series.Target.ContainerID = "", ""
			series.Target.StartedAt = series.Target.PodCreatedAt
			series.Origin, series.SampleClock = "cgroup-v2", "collector-capture"
		}
		_ = json.NewEncoder(w).Encode(struct {
			metav1.TypeMeta   `json:",inline"`
			metav1.ObjectMeta `json:"metadata"`
			History           memoryhistory.Report `json:"history"`
		}{metav1.TypeMeta{APIVersion: "memory.kubememlens.io/v1alpha1", Kind: "MemoryHistory"}, metav1.ObjectMeta{Namespace: "team-a", Name: "api", UID: "pod-uid"}, report})
	}))
	defer server.Close()
	config := kubeconfigForTLS(t, server)
	out, err := runRestrictedCLI(t, config, "deep", "history", "trends", "pod", "api", "-n", "team-a", "--source", "prometheus", "--window", "168h", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var r memoryhistory.Report
	if json.Unmarshal([]byte(out), &r) != nil || r.Query.Step != 42*time.Minute || len(r.Series[0].Points) != memoryhistory.MaxPoints {
		t.Fatal("CLI window or source contract lost")
	}
	out, err = runRestrictedCLI(t, config, "deep", "history", "trends", "pod", "api", "-n", "team-a", "--source", "local")
	if err != nil || !strings.Contains(out, "cgroup-charge") || !strings.Contains(out, "local") {
		t.Fatal("local fallback mislabelled", out, err)
	}
	deny.Store(true)
	out, err = runRestrictedCLI(t, config, "deep", "history", "trends", "pod", "api", "-n", "team-a", "--source", "prometheus")
	if err == nil || strings.Contains(out, "node-uid") || strings.Contains(out, "containerd://") {
		t.Fatal("denied history returned evidence")
	}
}

func TestHistoryTrendsRejectsQueryInjectionBeforeConnection(t *testing.T) {
	for _, args := range [][]string{
		{"history", "trends", "pod", "../other", "-n", "team-a"},
		{"history", "trends", "pod", "api", "--source", "arbitrary"},
		{"history", "trends", "pod", "api", "--metric", "up{}"},
		{"history", "trends", "pod", "api", "--window", "169h"},
		{"history", "trends", "pod", "api", "--step", "500ms"},
		{"history", "trends", "workload", "Secret/private"},
	} {
		_, err := runRestrictedCLI(t, "/missing-kubeconfig", "deep", args...)
		if err == nil || strings.Contains(err.Error(), "missing-kubeconfig") {
			t.Fatal("invalid query reached connection", err)
		}
	}
}

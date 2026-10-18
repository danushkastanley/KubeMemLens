package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMarkerCLIAndCaptureUseAuthenticatedContextAndRedactByDefault(t *testing.T) {
	var deny atomic.Bool
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-reader" {
			t.Error("reader identity lost")
		}
		if deny.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends-context" {
			t.Error("unexpected context path")
			http.NotFound(w, r)
			return
		}
		now := time.Now().UTC()
		query, err := memoryhistory.ParseQuery(r.URL.Query(), memoryhistory.Pod, now)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		selected := memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "team-a", Name: "api"}, UID: "private-pod-uid", ResolvedAt: now, Targets: []memoryhistory.Target{{Namespace: "team-a", Pod: "api", PodUID: "private-pod-uid", Container: "private-container", ContainerID: "containerd://private-runtime", Node: "private-node", NodeUID: "private-node-uid", StartedAt: now.Add(-time.Hour), PodCreatedAt: now.Add(-time.Hour)}}}
		history, err := memoryhistory.NewReport(selected, query, now)
		if err != nil {
			t.Error(err)
			return
		}
		history.Series[0].Origin = "cadvisor"
		history.Series[0].SampleClock = "prometheus-sample"
		at := query.End.Add(-30 * time.Second)
		marker := changemarkers.Marker{Kind: changemarkers.Resize, ResizeState: "completed", Clock: changemarkers.KubernetesEvent, SourceUID: "private-event", At: at, Until: at, Subject: changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: "api", UID: "private-pod-uid"}, Count: 1, Uncertain: true}
		changes, err := changemarkers.Compose(selected, query, now, changemarkers.Partial, []changemarkers.Marker{marker}, false)
		if err != nil {
			t.Error(err)
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			metav1.TypeMeta   `json:",inline"`
			metav1.ObjectMeta `json:"metadata"`
			Context           changemarkers.Context `json:"context"`
		}{metav1.TypeMeta{Kind: "MemoryHistoryContext", APIVersion: "memory.kubememlens.io/v1alpha1"}, metav1.ObjectMeta{Namespace: "team-a", Name: "api", UID: "private-pod-uid"}, changemarkers.Context{SchemaVersion: 1, History: history, Changes: changes}})
	}))
	defer server.Close()
	config := kubeconfigForTLS(t, server)
	out, err := runRestrictedCLI(t, config, "deep", "history", "trends", "pod", "api", "-n", "team-a", "--source", "prometheus", "--markers")
	if err != nil || !strings.Contains(out, "resize · completed") || !strings.Contains(out, "correlation does not establish causation") {
		t.Fatal(out, err)
	}
	path := filepath.Join(t.TempDir(), "history.json")
	capture := []string{"capture", "trends", "pod", "api", "-n", "team-a", "--source", "prometheus", "-o", path}
	if _, err := runRestrictedCLI(t, config, "deep", capture...); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "team-a") {
		t.Fatal("capture exposed raw identity")
	}
	document, err := incident.Read(path)
	if err != nil || document.History == nil || !document.History.Redacted {
		t.Fatal("invalid capture", err)
	}
	out, err = runRestrictedCLI(t, "/unused-kubeconfig", "deep", "replay", path)
	if err != nil || !strings.Contains(out, "resize · completed") {
		t.Fatal("offline replay lost markers", out, err)
	}
	out, err = runRestrictedCLI(t, "/unused-kubeconfig", "deep", "compare", "--trends", "--before", path, "--after", path)
	if err != nil || !strings.Contains(out, "identity-unavailable") || !strings.Contains(out, "no replacement is inferred") {
		t.Fatal("aliases invented continuity", out, err)
	}
	if _, err := runRestrictedCLI(t, "/unused-kubeconfig", "deep", "compare", "--volumes", "--before", path, "--after", path); err == nil {
		t.Fatal("volume selector silently changed to history comparison")
	}
	beforeCalls := calls.Load()
	deny.Store(true)
	if _, err := runRestrictedCLI(t, config, "deep", append(capture, "--force")...); err == nil {
		t.Fatal("revoked capture succeeded")
	}
	if calls.Load() != beforeCalls+1 {
		t.Fatal("overwrite did not reacquire authorised context")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("revoked overwrite changed previous capture")
	}
}

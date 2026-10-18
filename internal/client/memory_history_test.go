package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func clientHistoryFixture(t *testing.T) (memoryhistory.Report, memoryhistory.Request, memoryhistory.Query) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	request := memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "team-a", Name: "api"}
	s := memoryhistory.Selection{Request: request, UID: "pod-uid", ResolvedAt: now, Targets: []memoryhistory.Target{{Namespace: "team-a", Pod: "api", PodUID: "pod-uid", Container: "app", ContainerID: "containerd://current", Node: "node-a", NodeUID: "node-uid", StartedAt: now.Add(-time.Hour)}}}
	q := memoryhistory.Query{Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet, Start: now.Add(-time.Minute), End: now, Step: time.Minute}
	r, err := memoryhistory.NewReport(s, q, now)
	if err != nil {
		t.Fatal(err)
	}
	r.Series[0].Origin, r.Series[0].SampleClock = "cadvisor", "prometheus-sample"
	return r, request, q
}

func TestMemoryHistoryClientChecksScopeAndProvenance(t *testing.T) {
	for _, mutate := range []bool{false, true} {
		r, request, q := clientHistoryFixture(t)
		if mutate {
			r.Series[0].Target.PodUID = "another-instance"
		}
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, in *http.Request) {
			calls++
			if in.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends" || in.URL.Query().Get("source") != "prometheus" {
				t.Error("incorrect history route")
			}
			_ = json.NewEncoder(w).Encode(struct {
				metav1.TypeMeta   `json:",inline"`
				metav1.ObjectMeta `json:"metadata"`
				History           memoryhistory.Report `json:"history"`
			}{metav1.TypeMeta{APIVersion: "memory.kubememlens.io/v1alpha1", Kind: "MemoryHistory"}, metav1.ObjectMeta{Name: "api", Namespace: "team-a", UID: "pod-uid"}, r})
		}))
		defer server.Close()
		scope, _ := NamespaceScope("team-a")
		c := newTestKubernetesAPIClient(t, server.URL, scope)
		_, err := c.MemoryHistory(t.Context(), request, q)
		if (err != nil) != mutate {
			t.Fatalf("response validation: %v", err)
		}
		request.Namespace = "team-b"
		if _, err := c.MemoryHistory(t.Context(), request, q); err == nil || calls != 1 {
			t.Fatal("client scope escaped")
		}
	}
}

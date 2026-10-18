package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMemoryContextClientUsesOptInRouteAndValidatesEvidence(t *testing.T) {
	for _, change := range []string{"none", "other UID", "other query", "future markers", "invalid kind"} {
		t.Run(change, func(t *testing.T) {
			history, request, query := clientHistoryFixture(t)
			changes, err := changemarkers.Compose(history.Selection, query, history.ReceivedAt, changemarkers.Missing, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			result := changemarkers.Context{SchemaVersion: 1, History: history, Changes: changes}
			kind := "MemoryHistoryContext"
			switch change {
			case "other UID":
				result.Changes.UID = "replacement"
			case "other query":
				result.Changes.Start = result.Changes.End
			case "future markers":
				result.Changes.ObservedAt = time.Now().Add(time.Hour)
			case "invalid kind":
				kind = "MemoryHistory"
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends-context" || r.URL.Query().Get("source") != "prometheus" {
					t.Error("wrong context route")
				}
				_ = json.NewEncoder(w).Encode(struct {
					metav1.TypeMeta   `json:",inline"`
					metav1.ObjectMeta `json:"metadata"`
					Context           changemarkers.Context `json:"context"`
				}{metav1.TypeMeta{Kind: kind, APIVersion: "memory.kubememlens.io/v1alpha1"}, metav1.ObjectMeta{Name: "api", Namespace: "team-a", UID: "pod-uid"}, result})
			}))
			defer server.Close()
			scope, _ := NamespaceScope("team-a")
			client := newTestKubernetesAPIClient(t, server.URL, scope)
			_, err = client.MemoryHistoryContext(t.Context(), request, query)
			if (err != nil) != (change != "none") {
				t.Fatalf("untrusted context validation: %v", err)
			}
			request.Namespace = "team-b"
			if _, err := client.MemoryHistoryContext(t.Context(), request, query); err == nil || calls != 1 {
				t.Fatal("cross-tenant context acquired")
			}
		})
	}
}

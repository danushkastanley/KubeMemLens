package kube

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func TestOlderPodResponsesPreserveResourceAvailability(t *testing.T) {
	// These core/v1 response shapes remain valid on the 1.35 and 1.36 lanes.
	// Serve JSON through the production client, rather than constructing the
	// upgraded Kubernetes structs with their new fields already populated.
	for _, tc := range []struct {
		name       string
		resources  string
		limitKnown bool
		limitBytes uint64
	}{
		{"1.35 container budget", `{"requests":{"memory":"128Mi"},"limits":{"memory":"256Mi"}}`, true, 256 * 1024 * 1024},
		{"1.36 request without limit", `{"requests":{"memory":"128Mi"}}`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/pods" {
					t.Errorf("unexpected Kubernetes request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusNotFound)
					return
				}
				if r.URL.Query().Get("fieldSelector") != "spec.nodeName=node-a" {
					t.Errorf("node boundary missing from request: %s", r.URL.RawQuery)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{
					"apiVersion":"v1","kind":"PodList","metadata":{"resourceVersion":"10"},
					"items":[{
						"apiVersion":"v1","kind":"Pod",
						"metadata":{"namespace":"legacy","name":"app","uid":"uid-legacy"},
						"spec":{"nodeName":"node-a","containers":[{"name":"app","resources":%s}]},
						"status":{"phase":"Running","qosClass":"Burstable","containerStatuses":[{
							"name":"app","containerID":"containerd://abcdef1234567890",
							"restartCount":2,"state":{"running":{"startedAt":"2026-08-01T00:00:00Z"}}
						}]}
					}]
				}`, tc.resources)
			}))
			defer server.Close()
			client, err := NewClientForConfig(&rest.Config{Host: server.URL, Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			pods, err := ListPodsForNode(context.Background(), client, "node-a")
			if err != nil {
				t.Fatal(err)
			}
			if len(pods) != 1 {
				t.Fatalf("decoded %d Pods, want 1", len(pods))
			}
			pod := pods[0]
			if pod.Spec.Resources != nil || pod.Status.Resources != nil || pod.Status.VolumeHealth != nil {
				t.Fatal("absent Pod budget, allocated resources or volume health became reported")
			}
			ref, ok := BuildPodIndexFromPods(pods).Lookup("abcdef1234567890", "")
			if !ok || ref.Namespace != "legacy" || !ref.Running {
				t.Fatalf("legacy Pod did not survive the client/mapping boundary: %+v", ref)
			}
			ctx := ref.Context
			if !ctx.MemoryRequestKnown || ctx.MemoryRequestBytes != 128*1024*1024 {
				t.Fatalf("request context changed: %+v", ctx)
			}
			if ctx.MemoryLimitKnown != tc.limitKnown || ctx.MemoryLimitBytes != tc.limitBytes {
				t.Fatalf("limit availability changed: %+v", ctx)
			}
			if ctx.RestartCount != 2 || ctx.QoSClass != "Burstable" {
				t.Fatalf("existing status context changed: %+v", ctx)
			}
		})
	}
}

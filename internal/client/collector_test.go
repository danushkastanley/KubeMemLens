package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestCollectorClientTrimsBaseURLAndFetchesPods(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(api.ContainerPage{Items: []api.ContainerSnapshot{}})
	}))
	defer server.Close()

	client := NewCollectorClient(server.URL + "/")
	pods, err := client.Pods(context.Background())
	if err != nil {
		t.Fatalf("Pods returned error: %v", err)
	}
	if len(pods) != 0 {
		t.Fatalf("pods = %d, want 0", len(pods))
	}
	if gotPath != "/api/v1/pages/containers" {
		t.Fatalf("path = %q, want paged containers", gotPath)
	}
}

func TestCollectorClientFetchesNodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/nodes" {
			t.Fatalf("path = %q, want /api/v1/nodes", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"nodeName":"node-a","capturedAt":"2026-07-18T00:00:00Z","containerCount":2,"stale":false}]`))
	}))
	defer server.Close()

	nodes, err := NewCollectorClient(server.URL).Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if len(nodes) != 1 || nodes[0].NodeName != "node-a" || nodes[0].ContainerCount != 2 {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
}

func TestCollectorClientFetchesWorkloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pages/containers" {
			t.Fatalf("path = %q, want paged containers", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(api.ContainerPage{Items: []api.ContainerSnapshot{
			{Namespace: "default", PodName: "api-a", PodUID: "uid-a", ContainerName: "app", ContainerID: "id-a", Context: api.ContainerContext{WorkloadKind: "Deployment", WorkloadName: "api"}, Memory: model.MemoryBreakdown{TotalBytes: 100}},
			{Namespace: "default", PodName: "api-b", PodUID: "uid-b", ContainerName: "app", ContainerID: "id-b", Context: api.ContainerContext{WorkloadKind: "Deployment", WorkloadName: "api"}, Memory: model.MemoryBreakdown{TotalBytes: 300}},
		}})
	}))
	defer server.Close()
	workloads, err := NewCollectorClient(server.URL).Workloads(context.Background())
	if err != nil {
		t.Fatalf("Workloads returned error: %v", err)
	}
	if len(workloads) != 1 || workloads[0].Name != "api" || workloads[0].PodCount != 2 {
		t.Fatalf("unexpected workloads: %#v", workloads)
	}
}

func TestCollectorClientReportsUnusableResponse(t *testing.T) {
	tests := map[string]struct {
		respond func(http.ResponseWriter)
		fetch   func(*CollectorClient) error
		want    string
	}{
		"non-200 status includes the status": {
			respond: func(w http.ResponseWriter) { http.Error(w, "not ready", http.StatusServiceUnavailable) },
			fetch:   func(c *CollectorClient) error { _, err := c.Namespaces(context.Background()); return err },
			want:    "status 503",
		},
		"malformed JSON includes decode context": {
			respond: func(w http.ResponseWriter) { _, _ = w.Write([]byte(`not-json`)) },
			fetch:   func(c *CollectorClient) error { _, err := c.Containers(context.Background()); return err },
			want:    "decode",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { test.respond(w) }))
			defer server.Close()
			err := test.fetch(NewCollectorClient(server.URL))
			if err == nil || !contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCollectorClientHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("path = %q, want /healthz", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok\n"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := NewCollectorClient(server.URL).Health(ctx); err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
}

func TestCollectorClientPodHistory(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "node-a", CapturedAt: now,
		Containers: []api.ContainerSnapshot{{Namespace: "default", PodName: "api", PodUID: "uid-a", ContainerName: "app", ContainerID: "id-a", Memory: model.MemoryBreakdown{TotalBytes: 123}}},
	})
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(store, collector.DefaultHandlerOptions(time.Minute)))
	defer server.Close()
	history, err := NewCollectorClient(server.URL).PodHistory(context.Background(), "default", "api")
	if err != nil {
		t.Fatalf("PodHistory returned error: %v", err)
	}
	if len(history) != 1 || len(history[0].Points) != 1 || history[0].Points[0].TotalBytes != 123 {
		t.Fatalf("unexpected history: %#v", history)
	}
}

func contains(value string, needle string) bool {
	return strings.Contains(value, needle)
}

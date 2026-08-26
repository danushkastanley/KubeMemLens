package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"k8s.io/client-go/rest"
)

func TestKubernetesAPIClientUsesExactScopedResourcePaths(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.RequestURI())
		switch request.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods":
			writeTestJSON(t, w, api.PodMemoryList{Items: []api.PodMemory{{Snapshot: api.PodSnapshot{Namespace: "team-a", PodName: "api"}}}})
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/containers":
			writeTestJSON(t, w, api.ContainerMemoryList{})
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/workloads":
			writeTestJSON(t, w, api.WorkloadMemoryList{})
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api":
			writeTestJSON(t, w, api.PodMemory{Snapshot: api.PodSnapshot{Namespace: "team-a", PodName: "api"}})
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/history":
			writeTestJSON(t, w, api.PodMemoryHistory{Series: []api.PodHistory{{Namespace: "team-a", PodName: "api"}}})
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	scope, err := NamespaceScope("team-a")
	if err != nil {
		t.Fatal(err)
	}
	client := newTestKubernetesAPIClient(t, server.URL, scope)
	if _, err := client.Pods(context.Background()); err != nil {
		t.Fatalf("Pods returned error: %v", err)
	}
	if _, err := client.PodSummaries(context.Background()); err != nil {
		t.Fatalf("PodSummaries returned error: %v", err)
	}
	if _, err := client.Containers(context.Background()); err != nil {
		t.Fatalf("Containers returned error: %v", err)
	}
	if _, err := client.Workloads(context.Background()); err != nil {
		t.Fatalf("Workloads returned error: %v", err)
	}
	if _, err := client.Pod(context.Background(), "team-a", "api"); err != nil {
		t.Fatalf("Pod returned error: %v", err)
	}
	if _, err := client.PodHistory(context.Background(), "team-a", "api"); err != nil {
		t.Fatalf("PodHistory returned error: %v", err)
	}

	want := []string{
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods?limit=500",
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods?limit=500&summary=true",
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/containers?limit=500",
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/workloads?limit=500",
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api",
		"/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/history",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(requests, "\n"), strings.Join(want, "\n"))
	}
	for _, request := range requests {
		if strings.Contains(request, "/services/") || strings.Contains(request, "/proxy/") {
			t.Fatalf("aggregated client used service proxy path %q", request)
		}
	}
}

func TestKubernetesAPIClientUsesClusterPathsOnlyForAllNamespaces(t *testing.T) {
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		switch request.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1/pods":
			writeTestJSON(t, w, api.PodMemoryList{})
		case "/apis/memory.kubememlens.io/v1alpha1/nodes":
			writeTestJSON(t, w, api.NodeMemoryList{})
		case "/apis/memory.kubememlens.io/v1alpha1/clusterstatus/current":
			writeTestJSON(t, w, api.ClusterStatus{Store: api.DebugStore{Pods: 2}})
		case "/apis/memory.kubememlens.io/v1alpha1/metrics/current":
			writeTestJSON(t, w, api.Metrics{ContentType: "text/plain", Content: "metric 1\n"})
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	client := newTestKubernetesAPIClient(t, server.URL, AllNamespacesScope())
	if _, err := client.Pods(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Nodes(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := client.DebugStore(context.Background())
	if err != nil || status.Pods != 2 {
		t.Fatalf("DebugStore = %#v, %v", status, err)
	}
	metrics, err := client.Metrics(context.Background())
	if err != nil || metrics.Content != "metric 1\n" {
		t.Fatalf("Metrics = %#v, %v", metrics, err)
	}
	for _, path := range paths {
		if strings.Contains(path, "/namespaces/") {
			t.Fatalf("cluster request used namespaced path %q", path)
		}
	}
}

func TestKubernetesAPIClientCurrentSummarySkipsNestedResources(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.RequestURI())
		if request.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/pods" || request.URL.Query().Get("summary") != "true" {
			t.Fatalf("unexpected summary request %q", request.URL.RequestURI())
		}
		writeTestJSON(t, w, api.PodMemoryList{Items: []api.PodMemory{{Snapshot: api.PodSnapshot{Namespace: "team-a", PodName: "api"}}}})
	}))
	defer server.Close()

	client := newTestKubernetesAPIClient(t, server.URL, AllNamespacesScope())
	summary, err := client.CurrentSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Pods) != 1 || len(summary.Namespaces) != 1 || len(summary.Workloads) != 1 || len(requests) != 1 {
		t.Fatalf("summary = %#v, requests = %#v", summary, requests)
	}
}

func TestKubernetesAPIClientCurrentSnapshotDerivesAggregatesFromContainers(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.RequestURI())
		if request.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/containers" {
			t.Fatalf("unexpected complete snapshot request %q", request.URL.RequestURI())
		}
		writeTestJSON(t, w, api.ContainerMemoryList{Items: []api.ContainerMemory{{Snapshot: api.ContainerSnapshot{
			Namespace: "team-a", PodName: "api", PodUID: "uid-a", ContainerName: "app", NodeName: "worker",
			Context: api.ContainerContext{WorkloadKind: "Deployment", WorkloadName: "api"},
		}}}})
	}))
	defer server.Close()

	client := newTestKubernetesAPIClient(t, server.URL, AllNamespacesScope())
	snapshot, err := client.CurrentSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Containers) != 1 || len(snapshot.Pods) != 1 || len(snapshot.Namespaces) != 1 || len(snapshot.Workloads) != 1 || len(requests) != 1 {
		t.Fatalf("snapshot = %#v, requests = %#v", snapshot, requests)
	}
}

func TestKubernetesAPIClientPaginatesWithoutChangingScope(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.RequestURI())
		list := api.PodMemoryList{Items: []api.PodMemory{{Snapshot: api.PodSnapshot{Namespace: "team-a"}}}}
		if request.URL.Query().Get("continue") == "" {
			list.Continue = "next token"
		}
		writeTestJSON(t, w, list)
	}))
	defer server.Close()

	scope, _ := NamespaceScope("team-a")
	client := newTestKubernetesAPIClient(t, server.URL, scope)
	pods, err := client.Pods(context.Background())
	if err != nil {
		t.Fatalf("Pods returned error: %v", err)
	}
	if len(pods) != 2 {
		t.Fatalf("pods = %d, want 2", len(pods))
	}
	if len(requests) != 2 || requests[1] != "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods?continue=next+token&limit=500" {
		t.Fatalf("pagination requests = %#v", requests)
	}
}

func TestKubernetesAPIClientClassifiesErrorsWithoutResponseBody(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		check  func(error) bool
	}{
		{name: "forbidden", status: http.StatusForbidden, check: IsForbidden},
		{name: "not found", status: http.StatusNotFound, check: IsNotFound},
		{name: "unavailable", status: http.StatusServiceUnavailable, check: IsUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("tenant-secret object-name bearer-token"))
			}))
			defer server.Close()
			client := newTestKubernetesAPIClient(t, server.URL, AllNamespacesScope())
			_, err := client.Pods(context.Background())
			if !test.check(err) {
				t.Fatalf("error = %T %v", err, err)
			}
			for _, secret := range []string{"tenant-secret", "object-name", "bearer-token"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error contains response body value %q: %v", secret, err)
				}
			}
		})
	}
}

func TestKubernetesAPIClientDoesNotFallBackAfterAggregatedFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		if strings.Contains(request.URL.Path, "/services/") || strings.Contains(request.URL.Path, "/proxy/") {
			t.Fatalf("unexpected fallback request %q", request.URL.Path)
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newTestKubernetesAPIClient(t, server.URL, AllNamespacesScope())
	_, err := client.Pods(context.Background())
	if !IsUnavailable(err) {
		t.Fatalf("error = %v, want unavailable", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestKubernetesAPIClientUsesKubeconfigAuthenticationTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer scoped-token" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		writeTestJSON(t, w, api.PodMemoryList{})
	}))
	defer server.Close()

	client, err := NewKubernetesAPIClient(&rest.Config{Host: server.URL, BearerToken: "scoped-token"}, AllNamespacesScope(), time.Second)
	if err != nil {
		t.Fatalf("NewKubernetesAPIClient returned error: %v", err)
	}
	if _, err := client.Pods(context.Background()); err != nil {
		t.Fatalf("Pods returned error: %v", err)
	}
}

func TestKubernetesAPIClientRejectsCrossScopeObjectRequestLocally(t *testing.T) {
	scope, _ := NamespaceScope("team-a")
	client := &KubernetesAPIClient{scope: scope}
	_, err := client.Pod(context.Background(), "team-b", "api")
	if !IsForbidden(err) {
		t.Fatalf("error = %v, want forbidden", err)
	}
}

func TestKubernetesAPIClientTransportFailureIsUnavailable(t *testing.T) {
	client := newTestKubernetesAPIClient(t, "http://127.0.0.1:1", AllNamespacesScope())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := client.Pods(ctx)
	if !IsUnavailable(err) {
		t.Fatalf("error = %v, want unavailable", err)
	}
	var readErr *ReadError
	if !errors.As(err, &readErr) || readErr.Cause == nil {
		t.Fatalf("error = %#v, want wrapped transport cause", err)
	}
}

func newTestKubernetesAPIClient(t *testing.T, host string, scope ReadScope) *KubernetesAPIClient {
	t.Helper()
	client, err := NewKubernetesAPIClient(&rest.Config{Host: host}, scope, time.Second)
	if err != nil {
		t.Fatalf("NewKubernetesAPIClient returned error: %v", err)
	}
	return client
}

func writeTestJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

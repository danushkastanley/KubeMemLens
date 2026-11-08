package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func newAPITestClient(t *testing.T, discoveryStatus int) (*KubernetesAPIClient, *[]string) {
	t.Helper()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/apis/memory.kubememlens.io/v1alpha1" {
			w.WriteHeader(discoveryStatus)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	client, err := NewKubernetesAPIClient(&rest.Config{Host: server.URL}, ReadScope{Namespace: "tenant-a"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return client, &paths
}

func TestNotFoundWithAbsentAPIGroupReportsNotInstalled(t *testing.T) {
	client, paths := newAPITestClient(t, http.StatusNotFound)
	_, err := client.Pod(t.Context(), "tenant-a", "app")
	if !IsNotFound(err) || !IsAPINotInstalled(err) || len(*paths) != 2 {
		t.Fatalf("err=%v notInstalled=%v paths=%v", err, IsAPINotInstalled(err), *paths)
	}
	message := ConnectionError(Options{}, "", err).Error()
	if !strings.Contains(message, "KubeMemLens is not installed in this cluster") ||
		!strings.Contains(message, InstallGuideURL) || !strings.Contains(message, "--mode=restricted") ||
		strings.Contains(message, "\n") {
		t.Fatalf("unexpected message: %q", message)
	}
}

func TestNotFoundWithServedAPIGroupStaysObjectNotFound(t *testing.T) {
	client, _ := newAPITestClient(t, http.StatusOK)
	_, err := client.Pod(t.Context(), "tenant-a", "app")
	if !IsNotFound(err) || IsAPINotInstalled(err) {
		t.Fatalf("err=%v notInstalled=%v", err, IsAPINotInstalled(err))
	}
	if message := ConnectionError(Options{}, "", err).Error(); !strings.Contains(message, "object was not found") {
		t.Fatalf("unexpected message: %q", message)
	}
}

func TestUnavailableDiscoveryDoesNotClaimNotInstalled(t *testing.T) {
	client, _ := newAPITestClient(t, http.StatusServiceUnavailable)
	_, err := client.Pod(t.Context(), "tenant-a", "app")
	if !IsNotFound(err) || IsAPINotInstalled(err) {
		t.Fatalf("err=%v notInstalled=%v", err, IsAPINotInstalled(err))
	}
}

func TestHealthOnAbsentAPIGroupReportsNotInstalled(t *testing.T) {
	client, paths := newAPITestClient(t, http.StatusNotFound)
	if err := client.Health(t.Context()); !IsAPINotInstalled(err) || len(*paths) != 1 {
		t.Fatalf("err=%v paths=%v", err, *paths)
	}
}

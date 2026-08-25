package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewSnapshotReaderAutoPreservesExplicitCollectorURL(t *testing.T) {
	reader, description, err := NewSnapshotReader(context.Background(), Options{
		Mode:         ConnectionModeAuto,
		CollectorURL: "http://127.0.0.1:18080",
	})
	if err != nil {
		t.Fatalf("NewSnapshotReader returned error: %v", err)
	}
	if _, ok := reader.(*CollectorClient); !ok {
		t.Fatalf("reader = %T, want *CollectorClient", reader)
	}
	if description != "http://127.0.0.1:18080" {
		t.Fatalf("description = %q", description)
	}
}

func TestNewSnapshotReaderAutoUsesAggregatedAPIWithoutCollectorURL(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t)
	reader, description, err := NewSnapshotReader(context.Background(), Options{
		Mode:       ConnectionModeAuto,
		Kubeconfig: kubeconfig,
	})
	if err != nil {
		t.Fatalf("NewSnapshotReader returned error: %v", err)
	}
	if _, ok := reader.(*KubernetesAPIClient); !ok {
		t.Fatalf("reader = %T, want *KubernetesAPIClient", reader)
	}
	if description != "Kubernetes API memory.kubememlens.io/v1alpha1" {
		t.Fatalf("description = %q", description)
	}
}

func TestNewSnapshotReaderPreservesExplicitHTTPMode(t *testing.T) {
	reader, description, err := NewSnapshotReader(context.Background(), Options{
		Mode:         ConnectionModeHTTP,
		CollectorURL: "http://127.0.0.1:18080",
	})
	if err != nil {
		t.Fatalf("NewSnapshotReader returned error: %v", err)
	}
	if _, ok := reader.(*CollectorClient); !ok {
		t.Fatalf("reader = %T, want *CollectorClient", reader)
	}
	if description != "http://127.0.0.1:18080" {
		t.Fatalf("description = %q", description)
	}
}

func TestNewSnapshotReaderPreservesExplicitKubeProxyMode(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t)

	reader, description, err := NewSnapshotReader(context.Background(), Options{
		Mode:               ConnectionModeKubeProxy,
		CollectorNamespace: "kube-memlens",
		CollectorService:   "kube-memlens-collector",
		CollectorPort:      8080,
		Kubeconfig:         kubeconfig,
	})
	if err != nil {
		t.Fatalf("NewSnapshotReader returned error: %v", err)
	}
	if _, ok := reader.(*KubeProxyCollectorClient); !ok {
		t.Fatalf("reader = %T, want *KubeProxyCollectorClient", reader)
	}
	if description != "kube-proxy kube-memlens/kube-memlens-collector:8080" {
		t.Fatalf("description = %q", description)
	}
}

func TestNewSnapshotReaderInvalidMode(t *testing.T) {
	_, _, err := NewSnapshotReader(context.Background(), Options{Mode: "bad"})
	if err == nil {
		t.Fatal("NewSnapshotReader returned nil error for invalid mode")
	}
}

func writeTestKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	content := `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: http://127.0.0.1:65535
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user: {}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

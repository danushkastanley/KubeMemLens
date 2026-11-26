package client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNewSnapshotReaderSelectsReaderForMode(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t)
	tests := map[string]struct {
		options         Options
		wantReader      string
		wantDescription string
	}{
		"auto keeps an explicit collector URL": {
			options:    Options{Mode: ConnectionModeAuto, CollectorURL: "http://127.0.0.1:18080"},
			wantReader: "*client.CollectorClient", wantDescription: "http://127.0.0.1:18080",
		},
		"auto uses the aggregated API without a collector URL": {
			options:    Options{Mode: ConnectionModeAuto, Kubeconfig: kubeconfig},
			wantReader: "*client.KubernetesAPIClient", wantDescription: "Kubernetes API memory.kubememlens.io/v1alpha1",
		},
		"explicit HTTP mode": {
			options:    Options{Mode: ConnectionModeHTTP, CollectorURL: "http://127.0.0.1:18080"},
			wantReader: "*client.CollectorClient", wantDescription: "http://127.0.0.1:18080",
		},
		"explicit kube-proxy mode": {
			options: Options{
				Mode: ConnectionModeKubeProxy, CollectorNamespace: "kube-memlens", CollectorService: "kube-memlens-collector",
				CollectorPort: 8080, Kubeconfig: kubeconfig,
			},
			wantReader: "*client.KubeProxyCollectorClient", wantDescription: "kube-proxy kube-memlens/kube-memlens-collector:8080",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			reader, description, err := NewSnapshotReader(context.Background(), test.options)
			if err != nil {
				t.Fatalf("NewSnapshotReader returned error: %v", err)
			}
			if got := fmt.Sprintf("%T", reader); got != test.wantReader {
				t.Fatalf("reader = %s, want %s", got, test.wantReader)
			}
			if description != test.wantDescription {
				t.Fatalf("description = %q, want %q", description, test.wantDescription)
			}
		})
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

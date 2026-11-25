package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type recordedPaths struct {
	mu    sync.Mutex
	paths []string
}

func (r *recordedPaths) add(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, path)
}
func (r *recordedPaths) namespaces() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.paths, " ")
}

// apiServer answers every request with 404 and records what was asked for.
// The command checks its access first, and that review names the namespace.
func apiServer(t *testing.T) (*httptest.Server, *recordedPaths) {
	t.Helper()
	recorded := &recordedPaths{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		recorded.add(r.URL.Path + " " + string(body))
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server, recorded
}

func kubeconfigWithNamespace(t *testing.T, server, namespace string) string {
	t.Helper()
	contextNamespace := ""
	if namespace != "" {
		contextNamespace = "\n    namespace: " + namespace
	}
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
    user: test%s
current-context: test
users:
- name: test
  user:
    token: test
`, server, contextNamespace)
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runTopPods(t *testing.T, args ...string) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCommand(&out, &out)
	root.SetArgs(append([]string{"top", "pods", "--connect-mode=kubernetes-api"}, args...))
	// The server answers 404, so the command fails. Only the request matters.
	_ = root.Execute()
}

func TestNamespaceDefaultsToTheKubeconfigContext(t *testing.T) {
	t.Setenv("MEMLENS_COLLECTOR_URL", "")
	for _, tc := range []struct {
		name, context string
		args          []string
		want          string
	}{
		{"context namespace", "team-a", nil, `"namespace":"team-a"`},
		{"context without a namespace", "", nil, `"namespace":"default"`},
		{"explicit flag wins", "team-a", []string{"-n", "team-b"}, `"namespace":"team-b"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, recorded := apiServer(t)
			kubeconfig := kubeconfigWithNamespace(t, server.URL, tc.context)
			runTopPods(t, append([]string{"--kubeconfig", kubeconfig}, tc.args...)...)
			if !strings.Contains(recorded.namespaces(), tc.want) {
				t.Fatalf("requests %q do not ask about %s", recorded.namespaces(), tc.want)
			}
		})
	}
}

func TestRequestTimeoutIsBounded(t *testing.T) {
	t.Setenv("MEMLENS_COLLECTOR_URL", "")
	for value, ok := range map[string]bool{"1s": true, "5m": true, "999ms": false, "5m1s": false, "0s": false} {
		var out bytes.Buffer
		root := NewRootCommand(&out, &out)
		root.SetArgs([]string{"version", "--request-timeout=" + value})
		err := root.Execute()
		if (err == nil) != ok {
			t.Fatalf("--request-timeout=%s: err = %v, want accepted=%v", value, err, ok)
		}
		if err != nil && !strings.Contains(err.Error(), "--request-timeout") {
			t.Fatalf("--request-timeout=%s: error does not name the flag: %v", value, err)
		}
	}
}

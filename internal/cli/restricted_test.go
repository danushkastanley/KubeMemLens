package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	authorization "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func restrictedConfig(t *testing.T, metricStatus *atomic.Int32) string {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer fixture-reader" {
			t.Error("caller identity changed")
		}
		write := func(value any) {
			if err := json.NewEncoder(w).Encode(value); err != nil {
				t.Error(err)
			}
		}
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1":
			w.WriteHeader(404)
		case "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			var review authorization.SelfSubjectAccessReview
			if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
				t.Error(err)
			}
			attrs := review.Spec.ResourceAttributes
			if attrs == nil {
				t.Error("missing resource access review")
				w.WriteHeader(500)
				return
			}
			review.Status.Allowed = attrs.Namespace == "team-a"
			write(review)
		case "/api/v1/namespaces/team-a/pods":
			write(corev1.PodList{TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"}, Items: []corev1.Pod{{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "app", UID: "pod-uid", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)), Labels: map[string]string{"app": "demo", "private-label": "private-value"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}}}, {Name: "missing"}}}, Status: corev1.PodStatus{Phase: corev1.PodPending},
			}}})
		case "/apis/metrics.k8s.io":
			write(map[string]any{"name": "metrics.k8s.io", "versions": []any{map[string]string{"version": "v1beta1", "groupVersion": "metrics.k8s.io/v1beta1"}}})
		case "/apis/metrics.k8s.io/v1beta1":
			write(map[string]any{"groupVersion": "metrics.k8s.io/v1beta1", "resources": []any{map[string]any{"name": "pods", "namespaced": true, "verbs": []string{"list"}}}})
		case "/apis/metrics.k8s.io/v1beta1/namespaces/team-a/pods":
			if status := metricStatus.Load(); status != 0 {
				w.WriteHeader(int(status))
				return
			}
			write(map[string]any{"apiVersion": "metrics.k8s.io/v1beta1", "kind": "PodMetricsList", "items": []any{map[string]any{
				"metadata": map[string]string{"namespace": "team-a", "name": "app", "uid": "pod-uid"}, "timestamp": time.Now().UTC(), "window": "15s",
				"containers": []any{map[string]any{"name": "worker", "usage": map[string]string{"memory": "32Mi", "cpu": "0"}}},
			}}})
		default:
			t.Errorf("unexpected read: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	return kubeconfigForTLS(t, server)
}

func kubeconfigForTLS(t *testing.T, server *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	config := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: fixture
  cluster:
    server: %s
    certificate-authority-data: %s
contexts:
- name: fixture
  context:
    cluster: fixture
    user: reader
users:
- name: reader
  user:
    token: fixture-reader
current-context: fixture
`, server.URL, base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})))
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runRestrictedCLI(t *testing.T, config, mode string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	command := NewRootCommand(&out, &out)
	command.SetArgs(append([]string{"--kubeconfig", config, "--mode", mode}, args...))
	err := command.Execute()
	return out.String(), err
}

func TestRestrictedTopFormatsAndScopeUseTheRealReader(t *testing.T) {
	var status atomic.Int32
	config := restrictedConfig(t, &status)
	for _, mode := range []string{"restricted", "auto"} {
		for _, entity := range []string{"pods", "containers", "workloads", "ns"} {
			out, err := runRestrictedCLI(t, config, mode, "top", entity, "-n", "team-a", "-o", "json")
			if err != nil {
				t.Fatal(err)
			}
			var rows []map[string]any
			if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) == 0 {
				t.Fatalf("%s: %s %v", entity, out, err)
			}
			for _, row := range rows {
				if row["mode"] != "restricted" || row["totalBytes"] != nil || row["cgroup"] != nil {
					t.Fatal("memory meanings changed")
				}
			}
			for _, secret := range []string{"private-label", "private-value", "fixture-reader", "pod-uid"} {
				if strings.Contains(out, secret) {
					t.Fatalf("exported %s", secret)
				}
			}
		}
	}
	out, err := runRestrictedCLI(t, config, "restricted", "top", "pods", "-n", "team-a", "-o", "csv")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][6] != "33554432" {
		t.Fatalf("CSV: %s %v", out, err)
	}
	out, err = runRestrictedCLI(t, config, "restricted", "top", "pods", "-n", "team-a")
	if err != nil || !strings.Contains(out, "WORKING SET") || !strings.Contains(out, "kubernetes-metrics") || strings.Contains(out, "DIAGNOSIS") {
		t.Fatalf("table: %s %v", out, err)
	}
	if _, err = runRestrictedCLI(t, config, "restricted", "top", "pods", "-n", "team-b", "-o", "json"); err == nil {
		t.Fatal("cross-namespace access permitted")
	}
	if _, err = runRestrictedCLI(t, config, "restricted", "top", "pods", "-A"); err == nil {
		t.Fatal("cluster access permitted")
	}
	if _, err = runRestrictedCLI(t, config, "restricted", "top", "pods", "-n", "team-a", "--sort-by", "rss"); err == nil {
		t.Fatal("unavailable evidence used for sorting")
	}
}

func TestRestrictedReportsKeepDeepSchemaSeparateAndMissingMetricsExplicit(t *testing.T) {
	var status atomic.Int32
	config := restrictedConfig(t, &status)
	for _, operation := range []string{"explain", "recommend"} {
		out, err := runRestrictedCLI(t, config, "restricted", operation, "pod", "app", "-n", "team-a", "-o", "json")
		if err != nil {
			t.Fatal(err)
		}
		var document restrictedReport
		if err := json.Unmarshal([]byte(out), &document); err != nil {
			t.Fatal(err)
		}
		version := api.RestrictedExplanationSchemaVersion
		if operation == "recommend" {
			version = api.RestrictedRecommendationSchemaVersion
		}
		if document.SchemaVersion != version || document.Mode != "restricted" || document.AutomaticMutation || len(document.Children) != 2 {
			t.Fatalf("%s", out)
		}
		if strings.Contains(out, `"totalBytes"`) || strings.Contains(out, `"recentEvents"`) || strings.Contains(out, "private-value") {
			t.Fatal("restricted report fabricated deep evidence or exported labels")
		}
	}
	status.Store(403)
	out, err := runRestrictedCLI(t, config, "restricted", "top", "pods", "-n", "team-a", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"bytes": null`) || !strings.Contains(out, `"availability": "forbidden"`) {
		t.Fatalf("missing metrics: %s", out)
	}
}

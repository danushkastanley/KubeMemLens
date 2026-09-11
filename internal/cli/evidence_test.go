package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/collector"
)

func TestStatusRetainsDeepStoreAndAddsSharedPlan(t *testing.T) {
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(collector.NewStore(), collector.DefaultHandlerOptions(time.Minute)))
	defer server.Close()
	cmd := newStatusCommand(func() client.Options { return client.Options{CollectorURL: server.URL} })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--output=json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var report statusReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Store == nil || report.Evidence == nil || report.Evidence.Mode != capability.Deep || !report.Connection.Healthy {
		t.Fatalf("report=%+v", report)
	}
	if !strings.Contains(renderStatusReport(report), report.Evidence.Label()) {
		t.Fatal("status did not use shared label")
	}
}

func TestStatusRestrictedMissingMetricsIsPartialAndDoesNotListNamespaces(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			var review struct {
				Spec struct {
					ResourceAttributes struct {
						Namespace string
						Resource  string
						Verb      string
					}
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
				t.Error(err)
			}
			attrs := review.Spec.ResourceAttributes
			if attrs.Namespace != "tenant-a" || attrs.Resource != "pods" || attrs.Verb != "list" {
				t.Errorf("attributes=%+v", attrs)
			}
			fmt.Fprint(w, `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true}}`)
		case "/apis/metrics.k8s.io":
			w.WriteHeader(404)
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config")
	config := fmt.Sprintf(`{"apiVersion":"v1","kind":"Config","clusters":[{"name":"test","cluster":{"server":%q}}],"contexts":[{"name":"test","context":{"cluster":"test","user":"test"}}],"current-context":"test","users":[{"name":"test","user":{}}]}`, server.URL)
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := NewRootCommand(&out, &out)
	cmd.SetArgs([]string{"--mode=restricted", "--kubeconfig=" + path, "status", "-n", "tenant-a", "--output=json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var report statusReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Store != nil || report.Evidence.Mode != capability.Restricted || report.Data.Status != "partial" || len(paths) != 2 {
		t.Fatalf("%+v paths=%v", report, paths)
	}
	if report.Evidence.Sources[1].Reason != capability.SourceAbsent {
		t.Fatal("missing provider reason lost")
	}
	text := renderStatusReport(report)
	if strings.Contains(text, "Error:") || !strings.Contains(text, "current unavailable: query-not-implemented") {
		t.Fatalf("%s", text)
	}
}

func TestRestrictedModeNeverRunsDeepCommand(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand(&out, &out)
	cmd.SetArgs([]string{"--mode=restricted", "top", "pods"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "query-not-implemented") || strings.Contains(out.String(), "TOTAL") {
		t.Fatalf("output=%q err=%v", out.String(), err)
	}
}

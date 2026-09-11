package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	authorization "k8s.io/api/authorization/v1"
)

func evidenceOptions(t *testing.T, server *httptest.Server) Options {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
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
    user: test
current-context: test
users:
- name: test
  user: {}
`, server.URL)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	scope, err := NamespaceScope("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	return Options{Kubeconfig: path, ReadScope: scope, Timeout: time.Second}
}

func TestEvidenceSessionDeepUsesOnlyRequestedScope(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		switch r.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1":
			fmt.Fprint(w, `{}`)
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/tenant-a/pods":
			_ = json.NewEncoder(w).Encode(api.PodMemoryList{Items: []api.PodMemory{{Snapshot: api.PodSnapshot{
				Namespace: "tenant-a", PodName: "app", CapturedAt: time.Now(), Freshness: api.EvidenceFreshnessFresh, Completeness: api.EvidenceComplete,
			}}}})
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	session, err := NewEvidenceSession(context.Background(), evidenceOptions(t, server))
	if err != nil || session.Plan.Mode != capability.Deep || session.Plan.Freshness != capability.Fresh {
		t.Fatalf("%+v %v", session, err)
	}
	if len(paths) != 2 || session.Reader == nil {
		t.Fatalf("requests=%v", paths)
	}
}

func TestEvidenceSessionRestrictedDiscoveryNeverReadsObjects(t *testing.T) {
	for _, deepStatus := range []int{404, 403} {
		for _, version := range []string{"v1", "v1beta1"} {
			t.Run(fmt.Sprintf("%d-%s", deepStatus, version), func(t *testing.T) {
				var reviews []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/apis/memory.kubememlens.io/v1alpha1":
						w.WriteHeader(deepStatus)
					case "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
						if r.Method != "POST" {
							t.Errorf("method=%s", r.Method)
						}
						var review authorization.SelfSubjectAccessReview
						if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
							t.Error(err)
						}
						attrs := review.Spec.ResourceAttributes
						if attrs == nil || attrs.Namespace != "tenant-a" || attrs.Verb != "list" || attrs.Resource != "pods" {
							t.Errorf("scope=%+v", attrs)
							w.WriteHeader(500)
							return
						}
						reviews = append(reviews, attrs.Group)
						review.Status.Allowed = true
						_ = json.NewEncoder(w).Encode(review)
					case "/apis/metrics.k8s.io":
						fmt.Fprintf(w, `{"name":"metrics.k8s.io","versions":[{"version":%q,"groupVersion":%q}]}`, version, "metrics.k8s.io/"+version)
					case "/apis/metrics.k8s.io/" + version:
						fmt.Fprintf(w, `{"groupVersion":%q,"resources":[{"name":"pods","namespaced":true,"verbs":["list"]}]}`, "metrics.k8s.io/"+version)
					default:
						t.Errorf("unexpected object read %s", r.URL)
						w.WriteHeader(500)
					}
				}))
				defer server.Close()
				session, err := NewEvidenceSession(context.Background(), evidenceOptions(t, server))
				if err != nil || session.Plan.Mode != capability.Restricted || session.Reader != nil {
					t.Fatalf("%+v %v", session, err)
				}
				if !reflect.DeepEqual(reviews, []string{"", "metrics.k8s.io"}) {
					t.Fatalf("reviews=%v", reviews)
				}
				if session.Plan.Require(capability.Current) == nil {
					t.Fatal("restricted query enabled before reader/UI")
				}
				metric := session.Plan.Sources[2]
				if metric.APIVersion != "metrics.k8s.io/"+version || metric.Freshness != capability.UnknownFreshness {
					t.Fatalf("metrics=%+v", metric)
				}
			})
		}
	}
}

func TestEvidenceSessionDoesNotFallbackFromAuthenticationOrServerFailure(t *testing.T) {
	for _, status := range []int{401, 500, 302} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(status)
		}))
		_, err := NewEvidenceSession(context.Background(), evidenceOptions(t, server))
		server.Close()
		if err == nil || calls != 1 {
			t.Fatalf("status=%d calls=%d err=%v", status, calls, err)
		}
	}
}

func TestRestrictedModeRejectsCollectorAndInvalidModeBeforeIO(t *testing.T) {
	for _, opts := range []Options{
		{EvidenceMode: "node-context"},
		{EvidenceMode: capability.Restricted, CollectorURL: "http://example.invalid"},
		{EvidenceMode: capability.Restricted, Mode: ConnectionModeKubeProxy},
		{EvidenceMode: capability.Restricted, Mode: ConnectionModeHTTP},
	} {
		if _, err := NewEvidenceSession(context.Background(), opts); err == nil {
			t.Fatalf("accepted %+v", opts)
		}
	}
	if _, _, err := NewSnapshotReader(context.Background(), Options{EvidenceMode: capability.Restricted}); err == nil {
		t.Fatal("restricted flag silently constructed deep reader")
	}
}

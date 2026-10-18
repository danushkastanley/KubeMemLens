package kube

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type historyFixture struct {
	pod      corev1.Pod
	node     corev1.Node
	denied   string
	reads    []string
	access   []ObjectAccess
	resolver memoryhistory.Resolver
}

func newHistoryFixture(t *testing.T) *historyFixture {
	t.Helper()
	created := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	f := &historyFixture{
		pod:  corev1.Pod{TypeMeta: metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"}, ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "tenant-a", UID: "pod-uid", CreationTimestamp: metav1.NewTime(created)}, Spec: corev1.PodSpec{NodeName: "node-a"}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://current", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(created.Add(time.Minute))}}}}}},
		node: corev1.Node{TypeMeta: metav1.TypeMeta{Kind: "Node", APIVersion: "v1"}, ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: "node-uid", CreationTimestamp: metav1.NewTime(created.Add(-time.Hour))}},
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.reads = append(f.reads, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/tenant-a/pods/app":
			_ = json.NewEncoder(w).Encode(f.pod)
		case "/api/v1/nodes/node-a":
			_ = json.NewEncoder(w).Encode(f.node)
		default:
			t.Errorf("unexpected acquisition: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	var err error
	f.resolver, err = NewMemoryHistoryResolver(&rest.Config{Host: server.URL, TLSClientConfig: rest.TLSClientConfig{CAData: ca}}, func(_ context.Context, a ObjectAccess) error {
		f.access = append(f.access, a)
		key := a.Group + "/" + a.Resource
		if a.Subresource != "" {
			key += "/" + a.Subresource
		}
		if key == f.denied {
			return memoryhistory.ErrDenied
		}
		return nil
	}, func(name string, _ time.Time) (string, bool) { return "node-uid", name == "node-a" })
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestHistoryResolverBindsLivePodAndContainerLifetime(t *testing.T) {
	f := newHistoryFixture(t)
	s, err := f.resolver.Resolve(t.Context(), memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "tenant-a", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if s.UID != "pod-uid" || len(s.Targets) != 1 || s.Targets[0].NodeUID != "node-uid" || s.Targets[0].ContainerID != "containerd://current" || !s.Targets[0].PodCreatedAt.Equal(f.pod.CreationTimestamp.Time) {
		t.Fatalf("binding: %+v", s)
	}
}

func TestHistoryResolverChecksCallerBeforeAcquisition(t *testing.T) {
	for _, resource := range []string{"/pods", api.MemoryAPIGroup + "/pods"} {
		t.Run(resource, func(t *testing.T) {
			f := newHistoryFixture(t)
			f.denied = resource
			if _, err := f.resolver.Resolve(t.Context(), memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "tenant-a", Name: "app"}); !errors.Is(err, memoryhistory.ErrDenied) {
				t.Fatalf("authorisation: %v", err)
			}
			if len(f.reads) != 0 {
				t.Fatal("denied reader triggered acquisition")
			}
		})
	}
}

func TestHistoryResolverRejectsChangedAndInvalidTargets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*historyFixture)
	}{
		{"wrong namespace", func(f *historyFixture) { f.pod.Namespace = "tenant-b" }},
		{"missing UID", func(f *historyFixture) { f.pod.UID = "" }},
		{"deleted Pod", func(f *historyFixture) { now := metav1.Now(); f.pod.DeletionTimestamp = &now }},
		{"invalid node path", func(f *historyFixture) { f.pod.Spec.NodeName = "../../secrets" }},
		{"unknown container ID", func(f *historyFixture) { f.pod.Status.ContainerStatuses[0].ContainerID = "" }},
		{"container predates Pod", func(f *historyFixture) {
			f.pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(f.pod.CreationTimestamp.Add(-time.Second))
		}},
		{"duplicate container status", func(f *historyFixture) { f.pod.Status.InitContainerStatuses = f.pod.Status.ContainerStatuses }},
		{"no running container", func(f *historyFixture) { f.pod.Status.ContainerStatuses = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHistoryFixture(t)
			tc.change(f)
			if _, err := f.resolver.Resolve(t.Context(), memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "tenant-a", Name: "app"}); err == nil {
				t.Fatal("invalid lifetime accepted")
			}
		})
	}
}

func TestHistoryNodeReadRequiresNodePermission(t *testing.T) {
	f := newHistoryFixture(t)
	f.denied = "/nodes"
	request := memoryhistory.Request{Scope: memoryhistory.Node, Name: "node-a"}
	if _, err := f.resolver.Resolve(t.Context(), request); !errors.Is(err, memoryhistory.ErrDenied) {
		t.Fatal(err)
	}
	if len(f.reads) != 0 {
		t.Fatal("denied Node acquired")
	}
	f.denied = ""
	s, err := f.resolver.Resolve(t.Context(), request)
	if err != nil || s.UID != "node-uid" || len(s.Targets) != 1 || s.Targets[0].Pod != "" {
		t.Fatalf("Node-only selection: %+v %v", s, err)
	}
}

func TestHistoryUnknownNodeInventoryIsUnavailable(t *testing.T) {
	f := newHistoryFixture(t)
	f.resolver.(*memoryHistoryResolver).nodeIdentity = func(string, time.Time) (string, bool) { return "", false }
	if _, err := f.resolver.Resolve(t.Context(), memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "tenant-a", Name: "app"}); !errors.Is(err, memoryhistory.ErrUnavailable) {
		t.Fatalf("unknown identity: %v", err)
	}
}

func TestHistoryRechecksNamedSubresourcePermission(t *testing.T) {
	for _, tc := range []struct {
		request  memoryhistory.Request
		resource string
	}{
		{memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "tenant-a", Name: "app"}, "pods"},
		{memoryhistory.Request{Scope: memoryhistory.Container, Namespace: "tenant-a", Name: "app", Container: "app"}, "pods"},
		{memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: "app", WorkloadKind: "Deployment"}, "workloads"},
		{memoryhistory.Request{Scope: memoryhistory.Node, Name: "node-a"}, "nodes"},
	} {
		t.Run(string(tc.request.Scope), func(t *testing.T) {
			f := newHistoryFixture(t)
			f.denied = api.MemoryAPIGroup + "/" + tc.resource + "/trends"
			if _, err := f.resolver.Resolve(t.Context(), tc.request); !errors.Is(err, memoryhistory.ErrDenied) {
				t.Fatalf("revoked named history permission accepted: %v", err)
			}
			if len(f.reads) != 0 || len(f.access) != 1 {
				t.Fatal("denied history acquired objects")
			}
			a := f.access[0]
			if a.Name != tc.request.Name || a.Namespace != tc.request.Namespace || a.Subresource != "trends" {
				t.Fatal("history authorisation lost named scope")
			}
		})
	}
}

func TestNodeHistoryDoesNotRequireContainerInventoryPermission(t *testing.T) {
	f := newHistoryFixture(t)
	request := memoryhistory.Request{Scope: memoryhistory.Node, Name: "node-a"}
	f.denied = api.MemoryAPIGroup + "/nodes"
	if _, err := f.resolver.Resolve(t.Context(), request); err != nil {
		t.Fatalf("Node history depended on collector container inventory: %v", err)
	}
	if len(f.access) != 2 || f.access[0].Subresource != "trends" || f.access[1].Group != "" || f.access[1].Resource != "nodes" {
		t.Fatal("unexpected Node acquisition permissions")
	}
	f.denied = api.MemoryAPIGroup + "/nodes/trends"
	if _, err := f.resolver.Resolve(t.Context(), request); !errors.Is(err, memoryhistory.ErrDenied) {
		t.Fatalf("warm history permission revocation ignored: %v", err)
	}
}

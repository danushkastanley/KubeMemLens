package kube

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type markerFixture struct {
	config         *rest.Config
	extra          map[string]any
	mu             sync.Mutex
	now            time.Time
	pod            corev1.Pod
	rs, deployment workloadObject
	events         []map[string]any
	denied         map[string]bool
	reads          []string
	access         []ObjectAccess
	eventStatus    int
	beforeRead     func(*http.Request)
	eventRaw       []byte
	continued      bool
	provider       *changeMarkerProvider
	selection      memoryhistory.Selection
	query          memoryhistory.Query
}

func newMarkerFixture(t *testing.T) *markerFixture {
	t.Helper()
	return newMarkerFixtureAt(t, time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
}

func newMarkerFixtureAt(t *testing.T, now time.Time) *markerFixture {
	t.Helper()
	controller := true
	f := &markerFixture{now: now, denied: map[string]bool{}, eventStatus: 200}
	f.deployment = workloadObject{TypeMeta: metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"}, ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "app", UID: "deployment-uid", CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)), Annotations: map[string]string{"deployment.kubernetes.io/revision": "3"}}}
	f.rs = workloadObject{TypeMeta: metav1.TypeMeta{Kind: "ReplicaSet", APIVersion: "apps/v1"}, ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "app-rs", UID: "rs-uid", CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)), Annotations: map[string]string{"deployment.kubernetes.io/revision": "3"}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "app", UID: "deployment-uid", Controller: &controller}}}}
	f.pod = corev1.Pod{TypeMeta: metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"}, ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "app", UID: "pod-uid", CreationTimestamp: metav1.NewTime(now.Add(-4 * time.Minute)), OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "app-rs", UID: "rs-uid", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "node-a"}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://current", RestartCount: 1, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(now.Add(-3 * time.Minute))}}}}, Conditions: []corev1.PodCondition{{Type: corev1.PodResizeInProgress, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Minute)), Message: "PRIVATE CONDITION MESSAGE"}}}}
	f.selection = memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "tenant-a", Name: "app"}, UID: "pod-uid", ResolvedAt: now, Targets: []memoryhistory.Target{{Namespace: "tenant-a", Pod: "app", PodUID: "pod-uid", Container: "app", ContainerID: "containerd://current", Node: "node-a", NodeUID: "node-uid", StartedAt: now.Add(-3 * time.Minute), PodCreatedAt: now.Add(-4 * time.Minute)}}}
	f.query = memoryhistory.Query{Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet, Start: now.Add(-5 * time.Minute), End: now, Step: time.Minute}
	f.deployment.Spec.Selector = json.RawMessage(`{"matchLabels":{"app":"fixture"}}`)
	f.rs.Spec.Selector = json.RawMessage(`{"matchLabels":{"app":"fixture"}}`)
	f.pod.Labels = map[string]string{"app": "fixture"}
	f.events = []map[string]any{f.event("resize-event", "pod-uid", "ResizeCompleted")}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		hook := f.beforeRead
		f.mu.Unlock()
		if hook != nil {
			hook(r)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.reads = append(f.reads, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		var value any
		if extra, ok := f.extra[r.URL.Path+"?"+r.URL.Query().Get("labelSelector")]; ok {
			_ = json.NewEncoder(w).Encode(extra)
			return
		}
		if extra, ok := f.extra[r.URL.Path]; ok {
			_ = json.NewEncoder(w).Encode(extra)
			return
		}
		switch r.URL.Path {
		case "/api/v1/namespaces/tenant-a/pods":
			if r.URL.Query().Get("limit") != "33" || r.URL.Query().Get("labelSelector") != "app=fixture" {
				t.Error("unbounded or incorrect member list")
			}
			value = corev1.PodList{TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"}, Items: []corev1.Pod{f.pod}}
		case "/api/v1/namespaces/tenant-a/pods/app":
			value = f.pod
		case "/apis/apps/v1/namespaces/tenant-a/replicasets/app-rs":
			value = f.rs
		case "/apis/apps/v1/namespaces/tenant-a/deployments/app":
			value = f.deployment
		case "/api/v1/namespaces/tenant-a/events":
			if r.URL.Query().Get("limit") != "257" {
				t.Error("unbounded event acquisition")
			}
			if f.eventStatus != 200 {
				w.WriteHeader(f.eventStatus)
				return
			}
			if len(f.eventRaw) > 0 {
				_, _ = w.Write(f.eventRaw)
				return
			}
			metadata := map[string]string{}
			if f.continued {
				metadata["continue"] = "unread-page"
			}
			value = map[string]any{"kind": "EventList", "apiVersion": "v1", "metadata": metadata, "items": f.events}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	t.Cleanup(server.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	f.config = &rest.Config{Host: server.URL, TLSClientConfig: rest.TLSClientConfig{CAData: ca}}
	provider, err := NewChangeMarkerProvider(f.config, func(_ context.Context, a ObjectAccess) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.access = append(f.access, a)
		if f.denied[a.Group+"/"+a.Resource+"/"+a.Subresource] {
			return memoryhistory.ErrDenied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.provider = provider.(*changeMarkerProvider)
	f.provider.now = func() time.Time { return f.now }
	return f
}

func (f *markerFixture) event(uid, podUID, reason string) map[string]any {
	return map[string]any{"metadata": map[string]string{"uid": uid, "namespace": "tenant-a"}, "involvedObject": changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "tenant-a", Name: "app", UID: podUID}, "reason": reason, "eventTime": metav1.NewMicroTime(f.now.Add(-time.Minute)), "count": 1, "message": "PRIVATE EVENT MESSAGE", "reportingInstance": "PRIVATE INSTANCE", "action": "PRIVATE ACTION"}
}

func (f *markerFixture) edit(change func()) { f.mu.Lock(); defer f.mu.Unlock(); change() }

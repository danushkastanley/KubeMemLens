package agentless

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type currentFixture struct {
	at                                           time.Time
	pod                                          corev1.Pod
	denyPods, denyOwners, denyNodes, denyMetrics atomic.Bool
	ownerReads                                   atomic.Int32
}

func newCurrentFixture(t *testing.T, opts Options) (*Reader, *currentFixture) {
	t.Helper()
	f := &currentFixture{at: time.Now().UTC().Truncate(time.Second), pod: pendingPod("team-a", "app")}
	f.pod.Spec.NodeName = "node-a"
	f.pod.Spec.Containers[0].Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}}
	f.pod.Spec.Containers = append(f.pod.Spec.Containers, corev1.Container{Name: "sidecar"})
	f.pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "app-rs", UID: "rs-uid"}}
	server := httptest.NewServer(http.HandlerFunc(f.serve(t)))
	t.Cleanup(server.Close)
	opts.Now = func() time.Time { return f.at }
	reader, err := NewNamespace(&rest.Config{Host: server.URL, BearerToken: "caller-token"}, "team-a", opts)
	if err != nil {
		t.Fatal(err)
	}
	return reader, f
}

func (f *currentFixture) serve(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer caller-token" {
			t.Error("caller identity or method changed")
		}
		w.Header().Set("Content-Type", "application/json")
		write := func(value any) {
			if err := json.NewEncoder(w).Encode(value); err != nil {
				t.Error(err)
			}
		}
		switch r.URL.Path {
		case "/api/v1/namespaces/team-a/pods":
			if f.denyPods.Load() {
				w.WriteHeader(403)
				return
			}
			podListResponse(t, w, "", f.pod)
		case "/apis/apps/v1/namespaces/team-a/replicasets/app-rs":
			f.ownerReads.Add(1)
			if f.denyOwners.Load() {
				w.WriteHeader(403)
				return
			}
			write(appsv1.ReplicaSet{TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSet"}, ObjectMeta: metav1.ObjectMeta{
				Name: "app-rs", Namespace: "team-a", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "app", UID: "deployment-uid"}},
			}})
		case "/api/v1/nodes/node-a":
			if f.denyNodes.Load() {
				w.WriteHeader(403)
				return
			}
			write(corev1.Node{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Node"}, ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: "node-uid"}, Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")}, Allocatable: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse}},
			}})
		case "/apis/metrics.k8s.io":
			write(map[string]any{"name": "metrics.k8s.io", "versions": []any{map[string]string{"version": "v1beta1", "groupVersion": "metrics.k8s.io/v1beta1"}}})
		case "/apis/metrics.k8s.io/v1beta1":
			write(map[string]any{"groupVersion": "metrics.k8s.io/v1beta1", "resources": []any{
				map[string]any{"name": "pods", "namespaced": true, "verbs": []string{"list"}}, map[string]any{"name": "nodes", "namespaced": false, "verbs": []string{"get", "list"}},
			}})
		case "/apis/metrics.k8s.io/v1beta1/namespaces/team-a/pods":
			if f.denyMetrics.Load() {
				w.WriteHeader(403)
				return
			}
			write(map[string]any{"apiVersion": "metrics.k8s.io/v1beta1", "kind": "PodMetricsList", "items": []any{map[string]any{
				"metadata": map[string]string{"namespace": "team-a", "name": "app", "uid": string(f.pod.UID)}, "timestamp": f.at, "window": "15s",
				"containers": []any{map[string]any{"name": "worker", "usage": map[string]string{"memory": "32Mi", "cpu": "0"}}},
			}}})
		case "/apis/metrics.k8s.io/v1beta1/nodes/node-a":
			if f.denyMetrics.Load() {
				w.WriteHeader(403)
				return
			}
			write(map[string]any{"apiVersion": "metrics.k8s.io/v1beta1", "kind": "NodeMetrics", "metadata": map[string]string{"name": "node-a", "uid": "node-uid"},
				"timestamp": f.at, "window": "15s", "usage": map[string]string{"memory": "512Mi", "cpu": "0"}})
		default:
			t.Errorf("unexpected scope or endpoint: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}
}

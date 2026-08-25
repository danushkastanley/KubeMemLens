package extension

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func TestReadHandlerRequiresAuthenticationBeforeStoreAccess(t *testing.T) {
	handler := NewReadHandler(nil, collector.DefaultHandlerOptions(time.Minute))
	request := readRequest(t, "/apis/memory.kubememlens.io/v1alpha1/pods", false)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestReadHandlerNamespacedAndClusterPodLists(t *testing.T) {
	handler, _ := populatedReadHandler(t)
	namespaced := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods")
	if namespaced.Code != http.StatusOK {
		t.Fatalf("namespaced status=%d body=%s", namespaced.Code, namespaced.Body.String())
	}
	var namespacedList api.PodMemoryList
	decodeRead(t, namespaced, &namespacedList)
	if len(namespacedList.Items) != 1 || namespacedList.Items[0].Namespace != "team-a" || namespacedList.Items[0].Snapshot.Namespace != "team-a" {
		t.Fatalf("namespaced list = %#v", namespacedList)
	}

	cluster := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/pods")
	var clusterList api.PodMemoryList
	decodeRead(t, cluster, &clusterList)
	if len(clusterList.Items) != 2 {
		t.Fatalf("cluster list = %#v", clusterList)
	}
}

func TestReadHandlerDirectPodAndHistoryStayInRouteNamespace(t *testing.T) {
	handler, now := populatedReadHandler(t)
	direct := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api")
	var pod api.PodMemory
	decodeRead(t, direct, &pod)
	if pod.Namespace != "team-a" || pod.Snapshot.PodUID != "uid-a" {
		t.Fatalf("direct Pod = %#v", pod)
	}

	history := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/history")
	var series api.PodMemoryHistory
	decodeRead(t, history, &series)
	if len(series.Series) != 1 || series.Series[0].Namespace != "team-a" || !series.Series[0].Points[0].CapturedAt.Equal(now) {
		t.Fatalf("history = %#v", series)
	}

	missing := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-c/pods/api")
	assertStatus(t, missing, http.StatusNotFound, metav1.StatusReasonNotFound)
}

func TestReadHandlerScopeBindsContainerContinuation(t *testing.T) {
	handler, _ := populatedReadHandler(t)
	first := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/containers?limit=1")
	var page api.ContainerMemoryList
	decodeRead(t, first, &page)
	if len(page.Items) != 1 || page.Continue == "" {
		t.Fatalf("page = %#v", page)
	}
	crossScope := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/containers?continue="+page.Continue)
	assertStatus(t, crossScope, http.StatusBadRequest, metav1.StatusReasonBadRequest)
}

func TestReadHandlerClusterResources(t *testing.T) {
	handler, _ := populatedReadHandler(t)

	nodes := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/nodes")
	var nodeList api.NodeMemoryList
	decodeRead(t, nodes, &nodeList)
	if len(nodeList.Items) != 1 || nodeList.Items[0].Name != "node-a" {
		t.Fatalf("nodes = %#v", nodeList)
	}
	node := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/nodes/node-a")
	var nodeItem api.NodeMemory
	decodeRead(t, node, &nodeItem)
	if nodeItem.Name != "node-a" {
		t.Fatalf("node = %#v", nodeItem)
	}

	status := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/clusterstatus/current")
	var clusterStatus api.ClusterStatus
	decodeRead(t, status, &clusterStatus)
	if clusterStatus.Store.TotalContainers != 2 || clusterStatus.Store.Namespaces != 2 {
		t.Fatalf("cluster status = %#v", clusterStatus)
	}

	metrics := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/metrics/current")
	var metricResource api.Metrics
	decodeRead(t, metrics, &metricResource)
	if !strings.Contains(metricResource.Content, "kubememlens_namespace_memory_bytes") {
		t.Fatalf("metrics content missing namespace series: %q", metricResource.Content)
	}

	namespacedNodes := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/nodes")
	assertStatus(t, namespacedNodes, http.StatusNotFound, metav1.StatusReasonNotFound)
}

func TestReadHandlerRejectsWatchAndBoundsResponses(t *testing.T) {
	handler, _ := populatedReadHandler(t)
	watch := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods?watch=true")
	assertStatus(t, watch, http.StatusMethodNotAllowed, metav1.StatusReasonMethodNotAllowed)

	handler.opts.MaxResponseBytes = 16
	tooLarge := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods")
	assertStatus(t, tooLarge, http.StatusInsufficientStorage, metav1.StatusReason("ResponseTooLarge"))
}

func BenchmarkMetricsReadAtConfiguredCapacity(b *testing.B) {
	now := time.Unix(1_800_000_000, 0).UTC()
	containers := make([]api.ContainerSnapshot, collector.DefaultStoreLimits().MaxContainers)
	for index := range containers {
		containers[index] = api.ContainerSnapshot{
			Namespace: "target", PodName: fmt.Sprintf("pod-%06d", index), PodUID: fmt.Sprintf("uid-%06d", index),
			ContainerName: "app", ContainerID: fmt.Sprintf("container-%06d", index), NodeName: "node-a", CapturedAt: now,
		}
	}
	store := collector.NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
		b.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	handler := NewReadHandler(store, collector.DefaultHandlerOptions(time.Minute))
	handler.now = func() time.Time { return now }
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, readRequest(b, "/apis/memory.kubememlens.io/v1alpha1/metrics/current", true))
		if recorder.Code != http.StatusOK {
			b.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func populatedReadHandler(t *testing.T) (*ReadHandler, time.Time) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0).UTC()
	store := collector.NewStore()
	_, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "node-a", CapturedAt: now,
		Containers: []api.ContainerSnapshot{
			readContainer("team-a", "uid-a", "container-a", now),
			readContainer("team-b", "uid-b", "container-b", now),
		},
	})
	if err != nil {
		t.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	handler := NewReadHandler(store, collector.DefaultHandlerOptions(time.Minute))
	handler.now = func() time.Time { return now }
	return handler, now
}

func readContainer(namespace, podUID, containerID string, now time.Time) api.ContainerSnapshot {
	return api.ContainerSnapshot{
		Namespace: namespace, PodName: "api", PodUID: podUID, ContainerName: "app", ContainerID: containerID,
		NodeName: "node-a", CapturedAt: now,
		Context: api.ContainerContext{WorkloadKind: "Deployment", WorkloadName: "api"},
	}
}

func serveRead(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := readRequest(t, path, true)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func readRequest(t testing.TB, path string, authenticated bool) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	factory := &apirequest.RequestInfoFactory{
		APIPrefixes: sets.NewString("api", "apis"), GrouplessAPIPrefixes: sets.NewString("api"),
	}
	info, err := factory.NewRequestInfo(request)
	if err != nil {
		t.Fatalf("NewRequestInfo: %v", err)
	}
	ctx := apirequest.WithRequestInfo(request.Context(), info)
	if authenticated {
		ctx = apirequest.WithUser(ctx, &user.DefaultInfo{Name: "tenant-reader", Groups: []string{"system:authenticated"}})
	}
	return request.WithContext(ctx)
}

func decodeRead(t *testing.T, recorder *httptest.ResponseRecorder, out any) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func assertStatus(t *testing.T, recorder *httptest.ResponseRecorder, code int, reason metav1.StatusReason) {
	t.Helper()
	var status metav1.Status
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode Status: %v", err)
	}
	if recorder.Code != code || status.Reason != reason || status.Status != metav1.StatusFailure {
		t.Fatalf("status=%d response=%#v", recorder.Code, status)
	}
}

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
	summary := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods?summary=true")
	var summaryList api.PodMemoryList
	decodeRead(t, summary, &summaryList)
	if len(summaryList.Items) != 1 || len(summaryList.Items[0].Snapshot.Containers) != 0 ||
		summaryList.Items[0].Snapshot.Memory != namespacedList.Items[0].Snapshot.Memory {
		t.Fatalf("namespaced summary = %#v", summaryList)
	}
	invalidSummary := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods?summary=false")
	assertStatus(t, invalidSummary, http.StatusBadRequest, metav1.StatusReasonBadRequest)

	cluster := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/pods")
	var clusterList api.PodMemoryList
	decodeRead(t, cluster, &clusterList)
	if len(clusterList.Items) != 2 {
		t.Fatalf("cluster list = %#v", clusterList)
	}
}

func TestReadHandlerBindsPodSummaryContinuationToProjection(t *testing.T) {
	handler, _ := populatedReadHandler(t)
	first := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/pods?summary=true&limit=1")
	var page api.PodMemoryList
	decodeRead(t, first, &page)
	if len(page.Items) != 1 || page.Continue == "" {
		t.Fatalf("summary page = %#v", page)
	}
	full := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/pods?continue="+page.Continue)
	assertStatus(t, full, http.StatusBadRequest, metav1.StatusReasonBadRequest)
}

func TestReadHandlerPodSummaryBoundsFiveThousandContainers(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	containers := make([]api.ContainerSnapshot, 5000)
	for index := range containers {
		containers[index] = api.ContainerSnapshot{
			Namespace: "density", PodName: fmt.Sprintf("pod-%03d", index/50), PodUID: fmt.Sprintf("uid-%03d", index/50),
			ContainerName: fmt.Sprintf("container-%02d", index%50), ContainerID: fmt.Sprintf("runtime-%04d", index),
			NodeName: "worker", CapturedAt: now,
			Context: api.ContainerContext{WorkloadKind: "Deployment", WorkloadName: "density"},
		}
	}
	store := collector.NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "worker", CapturedAt: now, Containers: containers}); err != nil {
		t.Fatal(err)
	}
	handler := NewReadHandler(store, collector.DefaultHandlerOptions(time.Minute))
	handler.now = func() time.Time { return now }
	response := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/density/pods?summary=true")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Body.Len() >= 1<<20 || strings.Contains(response.Body.String(), "runtime-") {
		t.Fatalf("summary response retained nested container evidence: bytes=%d", response.Body.Len())
	}
	var summary api.PodMemoryList
	decodeRead(t, response, &summary)
	if len(summary.Items) != 100 {
		t.Fatalf("summaries = %d, want 100", len(summary.Items))
	}
	for _, pod := range summary.Items {
		if len(pod.Snapshot.Containers) != 0 {
			t.Fatalf("Pod %q retained %d containers", pod.Name, len(pod.Snapshot.Containers))
		}
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
	if series.Reliability.ResetAt.IsZero() || series.Reliability.Completeness != api.EvidencePartial {
		t.Fatalf("history reliability = %#v", series.Reliability)
	}

	missing := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-c/pods/api")
	assertStatus(t, missing, http.StatusNotFound, metav1.StatusReasonNotFound)
}

func TestReadHandlerReportsScopedHistoryCapacityLoss(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := collector.NewStoreWithHistory(collector.HistoryOptions{Duration: time.Hour, MaxSeries: 1, MaxPoints: 10})
	teamA := readContainer("team-a", "uid-a", "container-a", now)
	teamB := readContainer("team-b", "uid-b", "container-b", now.Add(time.Second))
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{teamA}})
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second), Containers: []api.ContainerSnapshot{teamA, teamB}})
	handler := NewReadHandler(store, collector.DefaultHandlerOptions(time.Minute))
	handler.now = func() time.Time { return now.Add(time.Second) }

	response := serveRead(t, handler, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-b/pods/api/history")
	var history api.PodMemoryHistory
	decodeRead(t, response, &history)
	if len(history.Series) != 0 || history.Reliability.DroppedSeries != 1 || history.Reliability.LastLossAt.IsZero() {
		t.Fatalf("scoped history loss = %#v", history)
	}
}

func TestReadHandlerRejectsContinuationFromPreviousCollectorGeneration(t *testing.T) {
	firstHandler, _ := populatedReadHandler(t)
	first := serveRead(t, firstHandler, "/apis/memory.kubememlens.io/v1alpha1/containers?limit=1")
	var page api.ContainerMemoryList
	decodeRead(t, first, &page)
	if page.Continue == "" {
		t.Fatal("expected continuation token")
	}
	secondHandler, _ := populatedReadHandler(t)
	response := serveRead(t, secondHandler, "/apis/memory.kubememlens.io/v1alpha1/containers?continue="+page.Continue)
	assertStatus(t, response, http.StatusBadRequest, metav1.StatusReasonBadRequest)
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
	if clusterStatus.Store.Reliability.State != api.CollectorReady || clusterStatus.Store.Reliability.Generation == "" {
		t.Fatalf("cluster reliability = %#v", clusterStatus.Store.Reliability)
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
		Environment: api.NodeEnvironment{NodeContextAvailable: true, WorkloadContextAvailable: true},
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

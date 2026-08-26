package extension

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/metrics"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

const readAPIVersion = api.MemoryAPIGroup + "/" + api.MemoryAPIVersion

type ReadHandler struct {
	store *collector.Store
	opts  collector.HandlerOptions
	now   func() time.Time
	gate  chan struct{}
}

func NewReadHandler(store *collector.Store, opts collector.HandlerOptions) *ReadHandler {
	return &ReadHandler{store: store, opts: opts, now: func() time.Time { return time.Now().UTC() }, gate: make(chan struct{}, 1)}
}

func (h *ReadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, authenticated := apirequest.UserFrom(r.Context())
	if !authenticated || principal == nil || strings.TrimSpace(principal.GetName()) == "" || principal.GetName() == "system:anonymous" {
		writeReadError(w, http.StatusUnauthorized, metav1.StatusReasonUnauthorized, "authentication is required")
		return
	}
	info, ok := apirequest.RequestInfoFrom(r.Context())
	if !ok || info == nil {
		writeReadError(w, http.StatusInternalServerError, metav1.StatusReasonInternalError, "request attributes are unavailable")
		return
	}
	if r.Method != http.MethodGet || !info.IsResourceRequest || info.APIGroup != api.MemoryAPIGroup || info.APIVersion != api.MemoryAPIVersion {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	if info.Verb != "get" && info.Verb != "list" {
		writeReadError(w, http.StatusMethodNotAllowed, metav1.StatusReasonMethodNotAllowed, "requested operation is not supported")
		return
	}
	select {
	case h.gate <- struct{}{}:
		defer func() { <-h.gate }()
	case <-r.Context().Done():
		writeReadError(w, http.StatusServiceUnavailable, metav1.StatusReasonServiceUnavailable, "read request was cancelled")
		return
	}

	switch info.Resource {
	case "pods":
		h.servePods(w, r, info)
	case "containers":
		h.serveContainers(w, r, info)
	case "workloads":
		h.serveWorkloads(w, r, info)
	case "nodes":
		h.serveNodes(w, r, info)
	case "clusterstatus":
		h.serveClusterStatus(w, info)
	case "metrics":
		h.serveMetrics(w, info)
	default:
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
	}
}

func (h *ReadHandler) servePods(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo) {
	if info.Subresource == "history" {
		if info.Verb != "get" || info.Namespace == "" || info.Name == "" || len(info.Parts) != 3 {
			writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
			return
		}
		now := h.now()
		series, reliability := h.store.ListPodHistoryWithReliability(info.Namespace, info.Name, "", now)
		if len(series) == 0 {
			if _, found := h.store.GetPod(info.Namespace, info.Name, now, h.opts.SnapshotTTL); !found {
				writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
				return
			}
		}
		writeBoundedReadJSON(w, api.PodMemoryHistory{
			TypeMeta:    metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "PodMemoryHistory"},
			ObjectMeta:  metav1.ObjectMeta{Name: info.Name, Namespace: info.Namespace},
			Series:      series,
			Reliability: reliability,
		}, h.opts.MaxResponseBytes)
		return
	}
	if info.Subresource != "" {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	scope := collector.ReadScope{Namespace: info.Namespace}
	if info.Verb == "list" && info.Name == "" {
		summary, err := podSummaryRequested(r.URL.Query().Get("summary"))
		if err != nil {
			writeReadError(w, http.StatusBadRequest, metav1.StatusReasonBadRequest, err.Error())
			return
		}
		var page collector.ScopedPodPage
		if summary {
			page, err = h.store.PagePodSummariesScoped(scope, h.now(), h.opts.SnapshotTTL, r.URL.Query(), h.generationScope(readScopeKey("pod-summaries", scope)))
		} else {
			page, err = h.store.PagePodsScoped(scope, h.now(), h.opts.SnapshotTTL, r.URL.Query(), h.generationScope(readScopeKey("pods", scope)), h.nestedReadBudget())
		}
		if writeReadPageError(w, err) {
			return
		}
		items := make([]api.PodMemory, len(page.Items))
		for index, pod := range page.Items {
			items[index] = podMemory(pod)
		}
		writeBoundedReadJSON(w, api.PodMemoryList{
			TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "PodMemoryList"}, ListMeta: metav1.ListMeta{Continue: page.Continue}, Items: items,
		}, h.opts.MaxResponseBytes)
		return
	}
	if info.Verb != "get" || info.Namespace == "" || info.Name == "" || len(info.Parts) != 2 {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	pod, found, err := h.store.GetPodBounded(info.Namespace, info.Name, h.now(), h.opts.SnapshotTTL, h.nestedReadBudget())
	if writeReadPageError(w, err) {
		return
	}
	if !found {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	writeBoundedReadJSON(w, podMemory(pod), h.opts.MaxResponseBytes)
}

func podSummaryRequested(value string) (bool, error) {
	switch value {
	case "":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("summary must be true when supplied")
	}
}

func (h *ReadHandler) serveContainers(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo) {
	if info.Verb != "list" || info.Name != "" || info.Subresource != "" {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	scope := collector.ReadScope{Namespace: info.Namespace}
	page, err := h.store.PageContainersScoped(scope, h.now(), h.opts.SnapshotTTL, r.URL.Query(), h.generationScope(readScopeKey("containers", scope)))
	if writeReadPageError(w, err) {
		return
	}
	items := make([]api.ContainerMemory, len(page.Items))
	for index, container := range page.Items {
		items[index] = containerMemory(container)
	}
	writeBoundedReadJSON(w, api.ContainerMemoryList{
		TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "ContainerMemoryList"},
		ListMeta: metav1.ListMeta{Continue: page.Continue}, Items: items,
	}, h.opts.MaxResponseBytes)
}

func (h *ReadHandler) serveWorkloads(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo) {
	if info.Verb != "list" || info.Name != "" || info.Subresource != "" {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	scope := collector.ReadScope{Namespace: info.Namespace}
	page, err := h.store.PageWorkloadsScoped(scope, h.now(), h.opts.SnapshotTTL, r.URL.Query(), h.generationScope(readScopeKey("workloads", scope)), h.nestedReadBudget())
	if writeReadPageError(w, err) {
		return
	}
	items := make([]api.WorkloadMemory, len(page.Items))
	for index, workload := range page.Items {
		items[index] = workloadMemory(workload)
	}
	writeBoundedReadJSON(w, api.WorkloadMemoryList{
		TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "WorkloadMemoryList"},
		ListMeta: metav1.ListMeta{Continue: page.Continue}, Items: items,
	}, h.opts.MaxResponseBytes)
}

func (h *ReadHandler) serveNodes(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo) {
	if info.Namespace != "" || info.Subresource != "" {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	if info.Verb == "list" && info.Name == "" {
		nodes := h.store.ListNodes(h.now(), h.opts.SnapshotTTL)
		keys := make([]string, len(nodes))
		for index, node := range nodes {
			keys[index] = node.NodeName
		}
		page, err := collector.PaginateKeys(keys, r.URL.Query(), h.generationScope("nodes:cluster"))
		if err != nil {
			writeReadError(w, http.StatusBadRequest, metav1.StatusReasonBadRequest, err.Error())
			return
		}
		items := make([]api.NodeMemory, len(page.Indexes))
		for offset, index := range page.Indexes {
			items[offset] = nodeMemory(nodes[index])
		}
		writeBoundedReadJSON(w, api.NodeMemoryList{
			TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "NodeMemoryList"},
			ListMeta: metav1.ListMeta{Continue: page.Continue}, Items: items,
		}, h.opts.MaxResponseBytes)
		return
	}
	if info.Verb != "get" || info.Name == "" || len(info.Parts) != 2 {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	node, found := h.store.GetNode(info.Name, h.now(), h.opts.SnapshotTTL)
	if !found {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	writeBoundedReadJSON(w, nodeMemory(node), h.opts.MaxResponseBytes)
}

func (h *ReadHandler) serveClusterStatus(w http.ResponseWriter, info *apirequest.RequestInfo) {
	if !exactClusterGet(info, "current") {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	store := h.store.Debug(h.now(), h.opts.SnapshotTTL)
	store.MaxResponseBytes = h.opts.MaxResponseBytes
	writeBoundedReadJSON(w, api.ClusterStatus{
		TypeMeta:   metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "ClusterStatus"},
		ObjectMeta: metav1.ObjectMeta{Name: "current"}, Store: store,
	}, h.opts.MaxResponseBytes)
}

func (h *ReadHandler) serveMetrics(w http.ResponseWriter, info *apirequest.RequestInfo) {
	if !exactClusterGet(info, "current") {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	if !h.opts.Metrics.Enabled {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	now := h.now()
	view, effectiveMetrics := h.store.BuildMetricsView(now, h.opts.SnapshotTTL, h.opts.Metrics, h.opts.MaxResponseBytes)
	content, err := (metrics.Exporter{
		Source: view, TTL: h.opts.SnapshotTTL, Now: func() time.Time { return now },
		Opts: effectiveMetrics, MaxBytes: h.nestedReadBudget(),
	}).Render()
	if err != nil {
		if errors.Is(err, metrics.ErrOutputTooLarge) {
			writeReadError(w, http.StatusInsufficientStorage, metav1.StatusReason("ResponseTooLarge"), "metrics exceed the configured maximum")
			return
		}
		writeReadError(w, http.StatusInternalServerError, metav1.StatusReasonInternalError, "metrics could not be rendered")
		return
	}
	writeBoundedReadJSON(w, api.Metrics{
		TypeMeta:   metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "Metrics"},
		ObjectMeta: metav1.ObjectMeta{Name: "current"}, ContentType: metrics.ContentType, Content: content,
	}, h.opts.MaxResponseBytes)
}

func exactClusterGet(info *apirequest.RequestInfo, name string) bool {
	return info.Namespace == "" && info.Verb == "get" && info.Name == name && info.Subresource == "" && len(info.Parts) == 2
}

func podMemory(snapshot api.PodSnapshot) api.PodMemory {
	return api.PodMemory{
		TypeMeta:   metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "PodMemory"},
		ObjectMeta: metav1.ObjectMeta{Name: snapshot.PodName, Namespace: snapshot.Namespace}, Snapshot: snapshot,
	}
}

func containerMemory(snapshot api.ContainerSnapshot) api.ContainerMemory {
	return api.ContainerMemory{
		TypeMeta:   metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "ContainerMemory"},
		ObjectMeta: metav1.ObjectMeta{Name: opaqueResourceName(snapshot.PodUID, snapshot.ContainerName, snapshot.ContainerID), Namespace: snapshot.Namespace},
		Snapshot:   snapshot,
	}
}

func workloadMemory(snapshot api.WorkloadSnapshot) api.WorkloadMemory {
	return api.WorkloadMemory{
		TypeMeta:   metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "WorkloadMemory"},
		ObjectMeta: metav1.ObjectMeta{Name: opaqueResourceName(strings.ToLower(snapshot.Kind), snapshot.Name), Namespace: snapshot.Namespace},
		Snapshot:   snapshot,
	}
}

func nodeMemory(snapshot api.NodeSnapshotStatus) api.NodeMemory {
	return api.NodeMemory{
		TypeMeta:   metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "NodeMemory"},
		ObjectMeta: metav1.ObjectMeta{Name: snapshot.NodeName}, Snapshot: snapshot,
	}
}

func opaqueResourceName(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func readScopeKey(resource string, scope collector.ReadScope) string {
	if scope.Namespace == "" {
		return resource + ":cluster"
	}
	return resource + ":namespace:" + scope.Namespace
}

func (h *ReadHandler) generationScope(scope string) string {
	return scope + ":" + h.store.Generation()
}

func (h *ReadHandler) nestedReadBudget() int {
	return h.opts.MaxResponseBytes / 2
}

func writeReadPageError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, collector.ErrReadPageTooLarge) {
		writeReadError(w, http.StatusInsufficientStorage, metav1.StatusReason("ResponseTooLarge"), "response exceeds the configured maximum")
		return true
	}
	writeReadError(w, http.StatusBadRequest, metav1.StatusReasonBadRequest, err.Error())
	return true
}

func writeBoundedReadJSON(w http.ResponseWriter, value any, maxBytes int) {
	writer := &limitedResponseCounter{remaining: maxBytes}
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		if errors.Is(err, errResponseTooLarge) {
			writeReadError(w, http.StatusInsufficientStorage, metav1.StatusReason("ResponseTooLarge"), "response exceeds the configured maximum")
			return
		}
		writeReadError(w, http.StatusInternalServerError, metav1.StatusReasonInternalError, "response could not be encoded")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(value)
}

var errResponseTooLarge = errors.New("response exceeds the configured maximum")

type limitedResponseCounter struct {
	remaining int
}

func (w *limitedResponseCounter) Write(data []byte) (int, error) {
	if w.remaining <= 0 || len(data) > w.remaining {
		return 0, errResponseTooLarge
	}
	w.remaining -= len(data)
	return len(data), nil
}

func writeReadError(w http.ResponseWriter, status int, reason metav1.StatusReason, message string) {
	writeJSON(w, status, metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusFailure, Reason: reason, Message: message, Code: int32(status),
	})
}

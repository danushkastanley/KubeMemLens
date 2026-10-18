package extension

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

type memoryHistoryService struct {
	resolver         memoryhistory.Resolver
	contextResolver  memoryhistory.Resolver
	markers          changemarkers.Provider
	local, remote    memoryhistory.Provider
	namespaces       map[string]bool
	nodes, workloads bool
	gate             chan struct{}
}

type memoryHistoryResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	History           memoryhistory.Report `json:"history"`
}

func (h *ReadHandler) serveMemoryHistory(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo) {
	w.Header().Set("Cache-Control", "no-store")
	s := h.memoryHistory
	if s == nil || info.Verb != "get" || info.Name == "" || len(info.Parts) != 3 {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "memory history is not configured for this resource")
		return
	}
	withChanges := info.Subresource == "trends-context"
	if withChanges && (s.markers == nil || s.contextResolver == nil || info.Resource == "nodes") {
		writeMemoryHistoryError(w, memoryhistory.ErrNotFound)
		return
	}
	if len(r.URL.RawQuery) > 4096 {
		writeMemoryHistoryError(w, memoryhistory.ErrInvalid)
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeMemoryHistoryError(w, memoryhistory.ErrInvalid)
		return
	}
	request := memoryhistory.Request{Namespace: info.Namespace, Name: info.Name}
	switch info.Resource {
	case "pods":
		request.Scope = memoryhistory.Pod
		if container := values.Get("container"); container != "" {
			request.Scope, request.Container = memoryhistory.Container, container
		}
	case "workloads":
		if !s.workloads {
			writeMemoryHistoryError(w, memoryhistory.ErrNotFound)
			return
		}
		request.Scope, request.WorkloadKind = memoryhistory.Workload, values.Get("kind")
	case "nodes":
		if !s.nodes {
			writeMemoryHistoryError(w, memoryhistory.ErrNotFound)
			return
		}
		request.Scope = memoryhistory.Node
	default:
		writeMemoryHistoryError(w, memoryhistory.ErrNotFound)
		return
	}
	if request.Scope != memoryhistory.Node && !s.namespaces[request.Namespace] {
		writeMemoryHistoryError(w, memoryhistory.ErrNotFound)
		return
	}
	if request.Validate() != nil || values.Get("container") != request.Container || values.Get("kind") != request.WorkloadKind {
		writeMemoryHistoryError(w, memoryhistory.ErrInvalid)
		return
	}
	query, err := memoryhistory.ParseQuery(values, request.Scope, h.now())
	if err != nil {
		writeMemoryHistoryError(w, err)
		return
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		writeReadError(w, http.StatusTooManyRequests, metav1.StatusReasonTooManyRequests, "a memory history query is already in progress")
		return
	}
	resolver, timeout := s.resolver, 9*time.Second
	if withChanges {
		resolver, timeout = s.contextResolver, 12*time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	var markerReader changemarkers.Provider
	if withChanges {
		markerReader = s.markers
	}
	acquired, err := s.acquire(ctx, request, query, resolver, markerReader)
	if err != nil {
		writeMemoryHistoryError(w, err)
		return
	}
	selected, report, changes := acquired.Selection, acquired.History, acquired.Changes
	if withChanges {
		h.writeHistoryContext(w, report, changes)
		return
	}
	writeBoundedReadJSON(w, memoryHistoryResource{TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "MemoryHistory"}, ObjectMeta: metav1.ObjectMeta{Namespace: request.Namespace, Name: request.Name, UID: types.UID(selected.UID)}, History: report}, min(h.opts.MaxResponseBytes, memoryhistory.MaxResponseBytes))
}

func writeMemoryHistoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, changemarkers.ErrUnsupported):
		writeReadError(w, http.StatusUnprocessableEntity, metav1.StatusReasonInvalid, "workload change context does not support this controller type or version; plain history remains available")
	case errors.Is(err, memoryhistory.ErrDenied):
		writeReadError(w, http.StatusForbidden, metav1.StatusReasonForbidden, "memory history access is denied")
	case errors.Is(err, memoryhistory.ErrNotFound):
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "memory history target was not found")
	case errors.Is(err, memoryhistory.ErrBounds):
		writeReadError(w, http.StatusRequestEntityTooLarge, metav1.StatusReasonRequestEntityTooLarge, "memory history exceeds its query budget; narrow the selection or window")
	case errors.Is(err, memoryhistory.ErrSource):
		writeReadError(w, http.StatusBadGateway, metav1.StatusReasonServiceUnavailable, "remote history source evidence is invalid")
	case errors.Is(err, memoryhistory.ErrInvalid):
		writeReadError(w, http.StatusBadRequest, metav1.StatusReasonBadRequest, "memory history request or source evidence is invalid")
	case errors.Is(err, memoryhistory.ErrChanged):
		writeReadError(w, http.StatusConflict, metav1.StatusReasonConflict, "memory history target changed; refresh the selection")
	default:
		writeReadError(w, http.StatusServiceUnavailable, metav1.StatusReasonServiceUnavailable, "memory history is unavailable; current live evidence remains separate")
	}
}

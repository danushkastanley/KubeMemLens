package extension

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

type replicaService struct {
	resolver   replicabaseline.Resolver
	evidence   func(context.Context, replicabaseline.Selection, time.Time) (replicabaseline.Input, error)
	namespaces map[string]bool
	gate       chan struct{}
}

type replicaResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Requested         changemarkers.Object   `json:"requested"`
	Baseline          replicabaseline.Report `json:"baseline"`
}

func (h *ReadHandler) serveReplicas(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo) {
	w.Header().Set("Cache-Control", "no-store")
	s := h.replicas
	if s == nil || info.Verb != "get" || info.Name == "" || len(info.Parts) != 3 || !s.namespaces[info.Namespace] {
		writeReplicaError(w, memoryhistory.ErrNotFound)
		return
	}
	request, err := replicaRequest(info, r.URL.RawQuery)
	if err != nil {
		writeReplicaError(w, err)
		return
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		writeReadError(w, http.StatusTooManyRequests, metav1.StatusReasonTooManyRequests, "a replica comparison is already in progress")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()
	selected, err := s.resolver.Resolve(ctx, request)
	if err != nil {
		writeReplicaError(w, err)
		return
	}
	if selected.Request != request || selected.Validate() != nil {
		writeReplicaError(w, replicabaseline.ErrInvalid)
		return
	}
	input, err := s.evidence(ctx, selected, h.now())
	if err != nil {
		writeReplicaError(w, err)
		return
	}
	if !replicaInputMatches(selected, input) {
		writeReplicaError(w, replicabaseline.ErrScope)
		return
	}
	current, err := s.resolver.Resolve(ctx, request)
	if err != nil {
		writeReplicaError(w, err)
		return
	}
	if current.Validate() != nil || !sameReplicaSelection(selected, current) {
		writeReplicaError(w, memoryhistory.ErrChanged)
		return
	}
	if err := ctx.Err(); err != nil {
		writeReplicaError(w, err)
		return
	}
	input.ObservedAt = h.now()
	report, err := replicabaseline.Analyse(input)
	if err != nil {
		writeReplicaError(w, err)
		return
	}
	if report.Workload != selected.Workload || report.Validate() != nil {
		writeReplicaError(w, replicabaseline.ErrInvalid)
		return
	}
	resource := replicaResource{TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "ReplicaBaseline"}, ObjectMeta: metav1.ObjectMeta{Namespace: request.Namespace, Name: request.Name, UID: types.UID(selected.Requested.UID)}, Requested: selected.Requested, Baseline: report}
	writeBoundedReadJSON(w, resource, min(h.opts.MaxResponseBytes, replicabaseline.MaxResponseBytes))
}

func replicaRequest(info *apirequest.RequestInfo, raw string) (memoryhistory.Request, error) {
	request := memoryhistory.Request{Namespace: info.Namespace, Name: info.Name}
	if len(raw) > 512 {
		return request, replicabaseline.ErrBounds
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return request, replicabaseline.ErrInvalid
	}
	switch info.Resource {
	case "pods":
		request.Scope = memoryhistory.Pod
	case "workloads":
		request.Scope = memoryhistory.Workload
		request.WorkloadKind = values.Get("kind")
	default:
		return request, memoryhistory.ErrNotFound
	}
	for key, items := range values {
		if key != "kind" || request.Scope != memoryhistory.Workload || len(items) != 1 {
			return request, replicabaseline.ErrInvalid
		}
	}
	if request.Validate() != nil {
		return request, replicabaseline.ErrInvalid
	}
	return request, nil
}

func sameReplicaSelection(a, b replicabaseline.Selection) bool {
	// Every second Resolve repeats named authorisation. Compare only structured
	// scope/membership/change evidence; acquisition clocks naturally advance.
	a.ResolvedAt = time.Time{}
	b.ResolvedAt = time.Time{}
	return reflect.DeepEqual(a, b)
}

func writeReplicaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, memoryhistory.ErrDenied):
		writeReadError(w, http.StatusForbidden, metav1.StatusReasonForbidden, "replica comparison access is denied")
	case errors.Is(err, memoryhistory.ErrNotFound):
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "replica comparison is not available for this resource")
	case errors.Is(err, memoryhistory.ErrChanged):
		writeReadError(w, http.StatusConflict, metav1.StatusReasonConflict, "replica membership or change evidence changed; refresh the selection")
	case errors.Is(err, replicabaseline.ErrBounds) || errors.Is(err, memoryhistory.ErrBounds):
		writeReadError(w, http.StatusRequestEntityTooLarge, metav1.StatusReasonRequestEntityTooLarge, "replica comparison exceeds the bounded peer or evidence limit")
	case errors.Is(err, changemarkers.ErrUnsupported):
		writeReadError(w, http.StatusUnprocessableEntity, metav1.StatusReasonInvalid, "replica comparison does not support this controller type or version")
	case errors.Is(err, replicabaseline.ErrInvalid) || errors.Is(err, replicabaseline.ErrScope) || errors.Is(err, memoryhistory.ErrInvalid):
		writeReadError(w, http.StatusBadRequest, metav1.StatusReasonBadRequest, "replica request or evidence is invalid")
	default:
		writeReadError(w, http.StatusServiceUnavailable, metav1.StatusReasonServiceUnavailable, "replica comparison is unavailable")
	}
}

func replicaInputMatches(selection replicabaseline.Selection, input replicabaseline.Input) bool {
	if input.Workload != selection.Workload || len(input.Peers) != len(selection.Members) {
		return false
	}
	members := map[string]changemarkers.Object{}
	for _, member := range selection.Members {
		members[member.Object.UID] = member.Object
	}
	for _, peer := range input.Peers {
		if members[peer.Object.UID] != peer.Object {
			return false
		}
		delete(members, peer.Object.UID)
	}
	return len(members) == 0
}

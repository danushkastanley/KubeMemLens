package extension

import (
	"errors"
	"net/http"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func (h *ReadHandler) serveNodeTopology(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo, schema int) {
	if !h.topologyEnabled || schema < api.TopologySnapshotSchemaVersion || info.Name == "" || info.Verb != "get" || len(info.Parts) != 3 {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	if len(r.URL.Query()) != 0 {
		writeReadError(w, http.StatusBadRequest, metav1.StatusReasonBadRequest, "topology reads do not accept query parameters")
		return
	}
	result, found, err := h.store.NodeTopology(info.Name, h.now())
	if errors.Is(err, collector.ErrNodeIdentityUnavailable) {
		writeReadError(w, http.StatusServiceUnavailable, metav1.StatusReasonServiceUnavailable, "current Node identity is unavailable")
		return
	}
	if err != nil {
		writeReadError(w, http.StatusInternalServerError, metav1.StatusReasonInternalError, "cannot read Node topology")
		return
	}
	if !found {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	result.TypeMeta = metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "NodeMemoryTopology"}
	result.ObjectMeta = metav1.ObjectMeta{Name: info.Name}
	writeBoundedReadJSON(w, result, min(h.opts.MaxResponseBytes, memorytopology.MaxReportBytes))
}

package extension

import (
	"net/http"

	"github.com/danushkastanley/kube-memlens/internal/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func (h *ReadHandler) serveNodeContexts(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo, schema int) {
	if schema < 3 || !h.nodeContextEnabled || info.Namespace != "" {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	if info.Subresource == "analysis" {
		h.serveNodeAnalysis(w, r, info)
		return
	}
	if info.Subresource == "history" && info.Name != "" && info.Verb == "get" && len(info.Parts) == 3 {
		result, err := h.store.PageNodeContextHistory(info.Name, h.now(), r.URL.Query(), h.nestedReadBudget())
		if writeReadPageError(w, err) {
			return
		}
		result.TypeMeta = metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "NodeContextHistory"}
		writeBoundedReadJSON(w, result, h.opts.MaxResponseBytes)
		return
	}
	if info.Subresource != "" {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	if info.Verb == "list" && info.Name == "" {
		records, continuation, err := h.store.PageNodeContexts(h.now(), r.URL.Query())
		if writeReadPageError(w, err) {
			return
		}
		items := make([]api.NodeContextResource, len(records))
		for i, record := range records {
			items[i] = nodeContextResource(record)
		}
		writeBoundedReadJSON(w, api.NodeContextList{TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "NodeContextList"},
			ListMeta: metav1.ListMeta{Continue: continuation}, Items: items}, h.opts.MaxResponseBytes)
		return
	}
	if info.Verb != "get" || info.Name == "" || len(info.Parts) != 2 {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	record, exists := h.store.GetNodeContext(info.Name, h.now())
	if !exists {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	writeBoundedReadJSON(w, nodeContextResource(record), h.opts.MaxResponseBytes)
}

func nodeContextResource(record api.NodeContextRecord) api.NodeContextResource {
	return api.NodeContextResource{TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "NodeContext"},
		ObjectMeta: metav1.ObjectMeta{Name: record.NodeName}, Record: record}
}

func (h *Handler) discoveryResources() []metav1.APIResource {
	resources := discoveryResources()
	if h.opts.NodeContextUsername != "" {
		resources = append(resources,
			metav1.APIResource{Name: "nodecontexts", SingularName: "nodecontext", Namespaced: false, Kind: "NodeContext", Verbs: metav1.Verbs{"get", "list"}},
			metav1.APIResource{Name: "nodecontexts/history", Namespaced: false, Kind: "NodeContextHistory", Verbs: metav1.Verbs{"get"}},
			metav1.APIResource{Name: "nodecontexts/analysis", Namespaced: false, Kind: "NodeMemoryAnalysis", Verbs: metav1.Verbs{"get"}})
	}
	return resources
}

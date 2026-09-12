package extension

import (
	"net/http"
	"strconv"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func (h *ReadHandler) serveNodeAnalysis(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo) {
	if info.Name == "" || info.Verb != "get" || len(info.Parts) != 3 {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	limit := nodeanalysis.DefaultContributors
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > nodeanalysis.MaxContributors {
			writeReadError(w, http.StatusBadRequest, metav1.StatusReasonBadRequest, "contributor limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	if h.podAuthorizer == nil {
		writeReadError(w, http.StatusServiceUnavailable, metav1.StatusReasonServiceUnavailable, "contributor authorisation is unavailable")
		return
	}
	principal, _ := apirequest.UserFrom(r.Context())
	decision, _, err := h.podAuthorizer.Authorize(r.Context(), authorizer.AttributesRecord{User: principal, Verb: "list", APIGroup: api.MemoryAPIGroup,
		APIVersion: api.MemoryAPIVersion, Resource: "pods", ResourceRequest: true})
	if err != nil {
		writeReadError(w, http.StatusServiceUnavailable, metav1.StatusReasonServiceUnavailable, "contributor authorisation is unavailable")
		return
	}
	access := nodeanalysis.NodeOnly
	if decision == authorizer.DecisionAllow {
		access = nodeanalysis.ClusterPods
	}
	now := h.now()
	record, found := h.store.GetNodeContext(info.Name, now)
	if !found {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	var qualification *nodeanalysis.Qualification
	if value, exists := h.accounting[record.NodeUID]; exists {
		qualification = &value
	}
	result, found, err := h.store.AnalyseNode(info.Name, access, qualification, nodeanalysis.Metric(r.URL.Query().Get("rank")), limit, now)
	if writeReadPageError(w, err) {
		return
	}
	if !found {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	writeBoundedReadJSON(w, api.NodeMemoryAnalysis{TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "NodeMemoryAnalysis"},
		ObjectMeta: metav1.ObjectMeta{Name: info.Name}, Analysis: result}, h.opts.MaxResponseBytes)
}

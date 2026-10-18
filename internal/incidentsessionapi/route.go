package incidentsessionapi

import (
	"net/http"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

type route struct {
	namespace, id string
	operation     incidentsession.Operation
}

func requestRoute(r *http.Request) (route, error) {
	info, ok := apirequest.RequestInfoFrom(r.Context())
	if !ok || info == nil {
		return route{}, incidentsession.ErrUnavailable
	}
	if !info.IsResourceRequest || info.APIGroup != api.MemoryAPIGroup || info.APIVersion != api.MemoryAPIVersion || info.Resource != Resource || info.Namespace == "" {
		return route{}, incidentsession.ErrNotFound
	}
	path := "/apis/" + api.MemoryAPIGroup + "/" + api.MemoryAPIVersion + "/namespaces/" + info.Namespace + "/" + Resource
	parts := 1
	if info.Name != "" {
		if !validID(info.Name) {
			return route{}, incidentsession.ErrInvalid
		}
		path += "/" + info.Name
		parts++
	}
	if info.Subresource != "" {
		path += "/" + info.Subresource
		parts++
	}
	if r.URL.Path != path || r.URL.RawPath != "" || r.URL.RawQuery != "" || len(info.Parts) != parts {
		return route{}, incidentsession.ErrInvalid
	}
	result := route{namespace: info.Namespace, id: info.Name}
	switch {
	case r.Method == http.MethodPost && info.Name == "" && info.Subresource == "":
		result.operation = incidentsession.Create
	case r.Method == http.MethodGet && info.Name != "" && info.Subresource == "":
		result.operation = incidentsession.Inspect
	case r.Method == http.MethodDelete && info.Name != "" && info.Subresource == "":
		result.operation = incidentsession.Delete
	case r.Method == http.MethodPost && info.Name != "" && info.Subresource == "markers":
		result.operation = incidentsession.Markers
	case r.Method == http.MethodPost && info.Name != "" && info.Subresource == "compare":
		result.operation = incidentsession.Compare
	case r.Method == http.MethodPost && info.Name != "" && info.Subresource == "capture":
		result.operation = incidentsession.Capture
	case r.Method == http.MethodPost && info.Name != "" && info.Subresource == incidentsession.TraceReferenceSubresource:
		result.operation = incidentsession.ReferenceTrace
	case r.Method == http.MethodPost && info.Name != "" && info.Subresource == "entries":
		result.operation = incidentsession.Append
	case r.Method == http.MethodPost && info.Name != "" && info.Subresource == "close":
		result.operation = incidentsession.CloseSession
	case r.Method == http.MethodGet && info.Name != "" && info.Subresource == "export":
		result.operation = incidentsession.ExportSanitised
	case r.Method == http.MethodGet && info.Name != "" && info.Subresource == "export-sensitive":
		result.operation = incidentsession.ExportAuthorised
	default:
		return route{}, incidentsession.ErrNotFound
	}
	verb, _, err := attributes(result.operation)
	if err != nil || info.Verb != verb {
		return route{}, incidentsession.ErrInvalid
	}
	return result, nil
}

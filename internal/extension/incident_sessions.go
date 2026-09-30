package extension

import (
	"context"
	"errors"
	"net/http"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/incidentsessionapi"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

// IncidentSessionOptions enables bounded, owner-only in-memory records for an
// explicit namespace allow-list. A nil option leaves storage and routes disabled.
type IncidentSessionOptions struct{ Namespaces []string }

func (o *IncidentSessionOptions) Validate() error {
	if o == nil {
		return nil
	}
	if incidentsessionapi.ValidateNamespaces(o.Namespaces) != nil {
		return errors.New("incident sessions require distinct valid namespaces")
	}
	return nil
}

func (h *Handler) configureIncidentSessions(ctx context.Context, kubeconfig string) error {
	o := h.opts.IncidentSessions
	if o == nil {
		return nil
	}
	if err := o.Validate(); err != nil {
		return err
	}
	config, err := kube.BuildConfig(kubeconfig, "")
	if err != nil {
		return errors.New("cannot configure incident namespace identity acquisition")
	}
	reader, err := kube.NewNamespaceIdentityReader(config)
	if err != nil {
		return errors.New("cannot configure verified incident namespace identity transport")
	}
	authority, err := incidentsessionapi.NewAuthority(o.Namespaces, reader, h.reads.podAuthorizer)
	if err != nil {
		reader.Close()
		return err
	}
	service, err := incidentsessionapi.NewHandler(ctx, authority, incidentsession.DefaultLimits(), &sessionCaptureSource{reads: h.reads, identity: reader})
	if err != nil {
		reader.Close()
		return err
	}
	context.AfterFunc(ctx, func() { service.Shutdown(); reader.Close() })
	h.incidentSessions = service
	return nil
}

func (h *Handler) serveResource(w http.ResponseWriter, r *http.Request) {
	info, ok := apirequest.RequestInfoFrom(r.Context())
	if h.incidentSessions != nil && ok && info != nil && info.Resource == incidentsessionapi.Resource {
		h.incidentSessions.ServeHTTP(w, r)
		return
	}
	h.reads.ServeHTTP(w, r)
}

func (h *Handler) incidentSessionResources() []metav1.APIResource {
	if h.opts.IncidentSessions == nil {
		return nil
	}
	return []metav1.APIResource{
		{Name: incidentsessionapi.Resource, SingularName: "incidentsession", Namespaced: true, Kind: "IncidentSession", Verbs: metav1.Verbs{"create", "get", "delete"}},
		{Name: incidentsessionapi.Resource + "/markers", Namespaced: true, Kind: "IncidentSession", Verbs: metav1.Verbs{"create"}},
		{Name: incidentsessionapi.Resource + "/compare", Namespaced: true, Kind: "IncidentSession", Verbs: metav1.Verbs{"create"}},
		{Name: incidentsessionapi.Resource + "/capture", Namespaced: true, Kind: "IncidentSession", Verbs: metav1.Verbs{"create"}},
		{Name: incidentsessionapi.Resource + "/" + incidentsession.TraceReferenceSubresource, Namespaced: true, Kind: "IncidentSession", Verbs: metav1.Verbs{"create"}},
		{Name: incidentsessionapi.Resource + "/entries", Namespaced: true, Kind: "IncidentSession", Verbs: metav1.Verbs{"create"}},
		{Name: incidentsessionapi.Resource + "/close", Namespaced: true, Kind: "IncidentSession", Verbs: metav1.Verbs{"create"}},
		{Name: incidentsessionapi.Resource + "/export", Namespaced: true, Kind: "IncidentSessionExport", Verbs: metav1.Verbs{"get"}},
		{Name: incidentsessionapi.Resource + "/export-sensitive", Namespaced: true, Kind: "IncidentSessionExport", Verbs: metav1.Verbs{"get"}},
	}
}

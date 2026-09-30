package client

import (
	"net/http"
	"net/url"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"k8s.io/client-go/rest"
)

type IncidentSessionProvider interface {
	OpenIncidentSessions(string) (IncidentSessions, error)
}

func verifiedIncidentTLS(config *rest.Config) bool {
	if config == nil || config.Insecure {
		return false
	}
	endpoint, err := url.Parse(config.Host)
	return err == nil && endpoint.Scheme == "https" && endpoint.Host != "" && endpoint.User == nil && endpoint.RawQuery == "" && endpoint.Fragment == ""
}

// OpenIncidentSessions retains this reader's cluster and credential transport.
// It never reloads a possibly changed current-context from kubeconfig.
func (c *KubernetesAPIClient) OpenIncidentSessions(namespace string) (IncidentSessions, error) {
	scope, err := NamespaceScope(namespace)
	if err != nil || !c.incidentTLS || !c.scope.allowsNamespace(scope.Namespace) {
		return nil, incidentsession.ErrDenied
	}
	api := *c
	api.scope = scope
	transport := *c.httpClient
	transport.Timeout = 12 * time.Second
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	api.httpClient = &transport
	return &IncidentClient{api: &api, namespace: scope.Namespace}, nil
}

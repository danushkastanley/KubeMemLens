package agent

import (
	"fmt"
	"net/http"
	"net/url"

	"k8s.io/client-go/rest"
)

// NewNodeContextPublisher applies the optional producer's strict transport
// contract while reusing epoch negotiation, sequence and retry behaviour.
func NewNodeContextPublisher(config *rest.Config) (*SnapshotPublisher, error) {
	if config == nil || config.Insecure {
		return nil, fmt.Errorf("Node-context publisher requires verified API TLS")
	}
	base, err := url.Parse(config.Host)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("Node-context publisher requires a qualified HTTPS API endpoint")
	}
	copied := rest.CopyConfig(config)
	copied.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	publisher, err := NewSnapshotPublisher(copied)
	if err != nil {
		return nil, err
	}
	publisher.client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return fmt.Errorf("Node-context publisher redirects are forbidden")
	}
	return publisher, nil
}

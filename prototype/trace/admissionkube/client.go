package admissionkube

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const MaxAPIResponseBytes = 4 << 20

type boundedTransport struct{ next http.RoundTripper }

// WrappedRoundTripper preserves client-go cancellation and TLS inspection through
// the response-size wrapper, including the bounded authentication watches.
func (t boundedTransport) WrappedRoundTripper() http.RoundTripper { return t.next }

func (t boundedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response.ContentLength > MaxAPIResponseBytes {
		response.Body.Close()
		return nil, errors.New("Kubernetes response exceeds trace admission limit")
	}
	response.Body = http.MaxBytesReader(nil, response.Body, MaxAPIResponseBytes)
	return response, nil
}

// NewClient bounds every Kubernetes response and request lifetime, including
// authentication configuration, delegated policy checks, Pods and Nodes.
func NewClient(config *rest.Config) (kubernetes.Interface, error) {
	if config == nil {
		return nil, errors.New("Kubernetes configuration required")
	}
	endpoint, err := url.Parse(config.Host)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || config.Insecure {
		return nil, errors.New("verified Kubernetes HTTPS configuration required")
	}
	bounded := rest.CopyConfig(config)
	bounded.Timeout = 5 * time.Second
	bounded.QPS = 20
	bounded.Burst = 20
	previous := bounded.WrapTransport
	bounded.WrapTransport = func(next http.RoundTripper) http.RoundTripper {
		if previous != nil {
			next = previous(next)
		}
		return boundedTransport{next: next}
	}
	return kubernetes.NewForConfig(bounded)
}

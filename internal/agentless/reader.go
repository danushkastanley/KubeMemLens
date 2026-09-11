package agentless

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Reader struct {
	client    kubernetes.Interface
	metrics   resourcemetrics.Source
	opts      Options
	namespace string
	refresh   chan struct{}
}

func NewNamespace(config *rest.Config, namespace string, opts Options) (*Reader, error) {
	if len(validation.IsDNS1123Label(namespace)) != 0 {
		return nil, fmt.Errorf("one valid namespace is required")
	}
	return newReader(config, namespace, opts)
}

func NewCluster(config *rest.Config, opts Options) (*Reader, error) {
	return newReader(config, "", opts)
}

func newReader(config *rest.Config, namespace string, opts Options) (*Reader, error) {
	if config == nil {
		return nil, fmt.Errorf("Kubernetes configuration is required")
	}
	base, err := url.Parse(config.Host)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("invalid Kubernetes API URL")
	}
	opts, err = normaliseOptions(opts)
	if err != nil {
		return nil, err
	}
	copied := rest.CopyConfig(config)
	copied.DisableCompression = true
	copied.ContentType = "application/json"
	copied.AcceptContentTypes = "application/json"
	previous := copied.WrapTransport
	copied.WrapTransport = func(base http.RoundTripper) http.RoundTripper {
		if previous != nil {
			base = previous(base)
		}
		return boundedTransport{base: base, opts: opts}
	}
	httpClient, err := rest.HTTPClientFor(copied)
	if err != nil {
		return nil, fmt.Errorf("create agentless transport: %w", err)
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client, err := kubernetes.NewForConfigAndClient(copied, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create agentless Kubernetes client: %w", err)
	}
	metricOptions := resourcemetrics.Options{Namespace: namespace, Timeout: opts.Timeout, MaxAge: opts.MaxAge, MaxResponseBytes: opts.MaxResponseBytes,
		PageSize: opts.PageSize, MaxPages: opts.MaxPages, MaxPods: opts.MaxPods, MaxContainers: opts.MaxContainers, MaxNodes: opts.MaxNodes, Now: opts.Now}
	var metrics resourcemetrics.Source
	if namespace == "" {
		metrics, err = resourcemetrics.NewCluster(copied, metricOptions)
	} else {
		metrics, err = resourcemetrics.New(copied, metricOptions)
	}
	if err != nil {
		return nil, err
	}
	return &Reader{client: client, metrics: metrics, opts: opts, namespace: namespace, refresh: make(chan struct{}, 1)}, nil
}

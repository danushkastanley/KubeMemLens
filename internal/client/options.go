package client

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCollectorNamespace = "kube-memlens"
	defaultCollectorService   = "kube-memlens-collector"
	defaultCollectorPort      = 8080
	defaultTimeout            = 5 * time.Second
)

type ConnectionMode string

const (
	ConnectionModeAuto          ConnectionMode = "auto"
	ConnectionModeKubernetesAPI ConnectionMode = "kubernetes-api"
	ConnectionModeHTTP          ConnectionMode = "http"
	ConnectionModeKubeProxy     ConnectionMode = "kube-proxy"
)

type Options struct {
	Mode      ConnectionMode
	ReadScope ReadScope

	CollectorURL string

	CollectorNamespace string
	CollectorService   string
	CollectorPort      int

	Kubeconfig string
	Context    string

	Timeout time.Duration
}

func DefaultOptions() Options {
	port := defaultCollectorPort
	if value := strings.TrimSpace(os.Getenv("MEMLENS_COLLECTOR_PORT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			port = parsed
		}
	}

	defaultScope, _ := NamespaceScope("default")
	return Options{
		Mode:               ConnectionModeAuto,
		ReadScope:          defaultScope,
		CollectorURL:       strings.TrimSpace(os.Getenv("MEMLENS_COLLECTOR_URL")),
		CollectorNamespace: envOrDefault("MEMLENS_COLLECTOR_NAMESPACE", defaultCollectorNamespace),
		CollectorService:   envOrDefault("MEMLENS_COLLECTOR_SERVICE", defaultCollectorService),
		CollectorPort:      port,
		Timeout:            defaultTimeout,
	}
}

func (o Options) WithDefaults() (Options, error) {
	if o.Mode == "" {
		o.Mode = ConnectionModeAuto
	}
	mode, err := ParseConnectionMode(string(o.Mode))
	if err != nil {
		return Options{}, err
	}
	o.Mode = mode
	if o.ReadScope == (ReadScope{}) {
		o.ReadScope, _ = NamespaceScope("default")
	}
	if err := o.ReadScope.validate(); err != nil {
		return Options{}, err
	}

	o.CollectorURL = strings.TrimSpace(o.CollectorURL)
	o.CollectorNamespace = strings.TrimSpace(o.CollectorNamespace)
	if o.CollectorNamespace == "" {
		o.CollectorNamespace = defaultCollectorNamespace
	}
	o.CollectorService = strings.TrimSpace(o.CollectorService)
	if o.CollectorService == "" {
		o.CollectorService = defaultCollectorService
	}
	if o.CollectorPort == 0 {
		o.CollectorPort = defaultCollectorPort
	}
	if o.CollectorPort < 0 {
		return Options{}, fmt.Errorf("collector port must be positive")
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	return o, nil
}

func ParseConnectionMode(value string) (ConnectionMode, error) {
	mode := ConnectionMode(strings.TrimSpace(value))
	switch mode {
	case "", ConnectionModeAuto:
		return ConnectionModeAuto, nil
	case ConnectionModeKubernetesAPI, ConnectionModeHTTP, ConnectionModeKubeProxy:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid connect mode %q, want auto, kubernetes-api, http, or kube-proxy", value)
	}
}

func ResolveMode(opts Options) (ConnectionMode, error) {
	opts, err := opts.WithDefaults()
	if err != nil {
		return "", err
	}
	if opts.Mode == ConnectionModeAuto {
		if opts.CollectorURL != "" {
			return ConnectionModeHTTP, nil
		}
		return ConnectionModeKubernetesAPI, nil
	}
	return opts.Mode, nil
}

func Describe(opts Options) string {
	opts, err := opts.WithDefaults()
	if err != nil {
		return "collector"
	}
	mode, err := ResolveMode(opts)
	if err != nil {
		return "collector"
	}
	if mode == ConnectionModeHTTP {
		if opts.CollectorURL == "" {
			return "http collector"
		}
		return opts.CollectorURL
	}
	if mode == ConnectionModeKubernetesAPI {
		return "Kubernetes API memory.kubememlens.io/v1alpha1"
	}
	return fmt.Sprintf("kube-proxy %s/%s:%d", opts.CollectorNamespace, opts.CollectorService, opts.CollectorPort)
}

func ConnectionError(opts Options, description string, err error) error {
	if IsForbidden(err) {
		return fmt.Errorf("You do not have permission to read KubeMemLens data in the requested scope")
	}
	if IsNotFound(err) {
		return fmt.Errorf("The requested KubeMemLens object was not found in the authorised scope")
	}
	if description == "" {
		description = Describe(opts)
	}
	opts, normaliseErr := opts.WithDefaults()
	if normaliseErr != nil {
		return normaliseErr
	}
	mode, resolveErr := ResolveMode(opts)
	if resolveErr != nil {
		return resolveErr
	}
	if mode == ConnectionModeKubeProxy {
		return fmt.Errorf("Could not connect to KubeMemLens collector through the Kubernetes API service proxy.\n\nChecked:\n  namespace: %s\n  service: %s\n  port: %d\n\nTry:\n  kubectl get svc -n %s %s\n  kubectl auth can-i get services/proxy -n %s\n  kubectl -n %s port-forward svc/%s 18080:%d\n  kubectl memlens top pods -A --collector-url=http://127.0.0.1:18080\n\nUnderlying error: %w",
			opts.CollectorNamespace,
			opts.CollectorService,
			opts.CollectorPort,
			opts.CollectorNamespace,
			opts.CollectorService,
			opts.CollectorNamespace,
			opts.CollectorNamespace,
			opts.CollectorService,
			opts.CollectorPort,
			err,
		)
	}
	if mode == ConnectionModeKubernetesAPI {
		if !IsUnavailable(err) {
			return fmt.Errorf("The KubeMemLens aggregated API returned an invalid response")
		}
		return fmt.Errorf("Could not reach the KubeMemLens aggregated API through Kubernetes.\n\nTry:\n  kubectl get apiservice v1alpha1.memory.kubememlens.io\n  kubectl get --raw /apis/memory.kubememlens.io/v1alpha1\n\nUnderlying error: %w", err)
	}
	return fmt.Errorf("Could not reach KubeMemLens collector at %s.\n\nTry:\n  kubectl -n kube-memlens port-forward svc/kube-memlens-collector 18080:8080\n\nUnderlying error: %w", description, err)
}

func envOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

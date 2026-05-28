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
	ConnectionModeAuto      ConnectionMode = "auto"
	ConnectionModeHTTP      ConnectionMode = "http"
	ConnectionModeKubeProxy ConnectionMode = "kube-proxy"
)

type Options struct {
	Mode ConnectionMode

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

	return Options{
		Mode:               ConnectionModeAuto,
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
	case ConnectionModeHTTP, ConnectionModeKubeProxy:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid connect mode %q, want auto, http, or kube-proxy", value)
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
		return ConnectionModeKubeProxy, nil
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
	return fmt.Sprintf("kube-proxy %s/%s:%d", opts.CollectorNamespace, opts.CollectorService, opts.CollectorPort)
}

func ConnectionError(opts Options, description string, err error) error {
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
	return fmt.Errorf("Could not reach KubeMemLens collector at %s.\n\nTry:\n  kubectl -n kube-memlens port-forward svc/kube-memlens-collector 18080:8080\n\nUnderlying error: %w", description, err)
}

func envOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

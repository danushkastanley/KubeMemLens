package client

import (
	"context"
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/kube"
)

func NewSnapshotReader(ctx context.Context, opts Options) (SnapshotReader, string, error) {
	opts, err := opts.WithDefaults()
	if err != nil {
		return nil, "", err
	}

	mode, err := ResolveMode(opts)
	if err != nil {
		return nil, "", err
	}

	switch mode {
	case ConnectionModeHTTP:
		if opts.CollectorURL == "" {
			return nil, "", fmt.Errorf("collector URL is required in http mode")
		}
		description := opts.CollectorURL
		return NewCollectorClientWithTimeout(opts.CollectorURL, opts.Timeout), description, nil
	case ConnectionModeKubeProxy:
		description := Describe(opts)
		config, err := kube.BuildConfig(opts.Kubeconfig, opts.Context)
		if err != nil {
			return nil, description, err
		}
		select {
		case <-ctx.Done():
			return nil, description, ctx.Err()
		default:
		}
		reader, err := NewKubeProxyCollectorClient(config, opts.CollectorNamespace, opts.CollectorService, opts.CollectorPort, opts.Timeout)
		if err != nil {
			return nil, description, err
		}
		return reader, description, nil
	default:
		return nil, "", fmt.Errorf("unsupported connect mode %q", mode)
	}
}

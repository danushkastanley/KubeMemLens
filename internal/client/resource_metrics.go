package client

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
)

// NewResourceMetricsSource is opt-in. Existing cgroup readers never call it.
// It uses the operator's Kubernetes identity, independently from collector access.
func NewResourceMetricsSource(opts Options) (resourcemetrics.Source, error) {
	normalised, err := opts.WithDefaults()
	if err != nil {
		return nil, err
	}
	mode, err := ResolveMode(normalised)
	if err != nil {
		return nil, err
	}
	if mode != ConnectionModeKubernetesAPI {
		return nil, fmt.Errorf("resource metrics require a Kubernetes API connection")
	}
	scope := normalised.ReadScope
	if scope.AllNamespaces {
		return nil, fmt.Errorf("resource metrics currently require one explicit namespace")
	}
	config, err := kube.BuildConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return nil, err
	}
	return resourcemetrics.New(config, resourcemetrics.Options{Namespace: scope.Namespace, Timeout: normalised.Timeout})
}

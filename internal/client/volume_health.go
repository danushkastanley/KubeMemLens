package client

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

// NewVolumeHealthSource is an opt-in reader using the operator's Kubernetes
// identity. Existing collector, capture and UI paths do not invoke it.
func NewVolumeHealthSource(opts Options) (volumehealth.SourceReader, error) {
	normalised, err := opts.WithDefaults()
	if err != nil {
		return nil, err
	}
	mode, err := ResolveMode(normalised)
	if err != nil {
		return nil, err
	}
	if mode != ConnectionModeKubernetesAPI || normalised.ReadScope.AllNamespaces {
		return nil, fmt.Errorf("volume health requires a Kubernetes API connection and one namespace")
	}
	config, err := kube.BuildConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return nil, err
	}
	return kube.NewVolumeHealthSource(config, kube.VolumeHealthOptions{Namespace: normalised.ReadScope.Namespace, Timeout: normalised.Timeout})
}

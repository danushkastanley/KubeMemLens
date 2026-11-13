package client

import (
	"k8s.io/client-go/rest"

	"github.com/danushkastanley/kube-memlens/internal/kube"
)

// KubeConfigError reports that the local Kubernetes client configuration could
// not be loaded. It is a problem on the caller's machine, not a statement about
// which evidence sources the cluster offers, so it is never folded into source
// discovery.
type KubeConfigError struct {
	Cause error
}

func (e *KubeConfigError) Error() string {
	return "Could not load the Kubernetes client configuration: " + e.Cause.Error() +
		"\n\nCheck --kubeconfig, --context and the KUBECONFIG environment variable."
}

func (e *KubeConfigError) Unwrap() error { return e.Cause }

func loadKubeConfig(opts Options) (*rest.Config, error) {
	config, err := kube.BuildConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return nil, &KubeConfigError{Cause: err}
	}
	return config, nil
}

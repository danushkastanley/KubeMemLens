package client

import (
	"context"
	"net/http"
)

// InstallGuideURL is the public installation guide shown in first-run hints.
const InstallGuideURL = "https://github.com/danushkastanley/KubeMemLens/blob/main/docs/installation.md"

const apiNotInstalledMessage = "KubeMemLens is not installed in this cluster: the memory.kubememlens.io/v1alpha1 API was not found. " +
	"Install the chart (" + InstallGuideURL + "), or use --mode=restricted for Metrics API working set."

// notFoundError separates a missing object from a missing API. The Kubernetes
// API server answers 404 for both, so a 404 on an object path is followed by
// one discovery request, whose result is kept on the error for reuse.
func (c *KubernetesAPIClient) notFoundError(ctx context.Context, operation, path string) error {
	err := &ReadError{Kind: ReadErrorNotFound, Operation: operation, StatusCode: http.StatusNotFound}
	if path == "" {
		err.APINotInstalled = true
		return err
	}
	err.discoveryErr = c.Health(ctx)
	err.discoveryChecked = true
	err.APINotInstalled = IsAPINotInstalled(err.discoveryErr)
	return err
}

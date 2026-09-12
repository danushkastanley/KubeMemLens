package client

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"k8s.io/apimachinery/pkg/util/validation"
)

type WorkloadVolumeReader interface {
	WorkloadVolumes(context.Context, string, string, string) (api.WorkloadVolumeContext, error)
}

func (c *KubernetesAPIClient) WorkloadVolumes(ctx context.Context, namespace, kind, name string) (api.WorkloadVolumeContext, error) {
	kind, ok := kube.CanonicalVolumeWorkloadKind(kind)
	if !ok || len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(name)) != 0 || !c.scope.allowsNamespace(namespace) {
		return api.WorkloadVolumeContext{}, fmt.Errorf("valid workload and authorised namespace are required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var result api.WorkloadVolumeContext
	path := "/namespaces/" + url.PathEscape(namespace) + "/workloads/" + url.PathEscape(name) + "/volumes?kind=" + url.QueryEscape(kind)
	// The bounded workload composition has a longer budget than an ordinary
	// snapshot GET. Share its authenticated transport without mutating clients.
	scoped := *c
	httpClient := *c.httpClient
	httpClient.Timeout = 10 * time.Second
	scoped.httpClient = &httpClient
	if err := scoped.getBounded(ctx, "get workload volumes", path, &result, volumecontext.MaxPageBytes); err != nil {
		return api.WorkloadVolumeContext{}, err
	}
	if result.Namespace != namespace || result.Name != name || result.Workload.Kind != kind {
		return api.WorkloadVolumeContext{}, readDecodeError("get workload volumes", fmt.Errorf("workload scope changed"))
	}
	if err := api.ValidateWorkloadVolumeContext(result, time.Now().UTC()); err != nil {
		return api.WorkloadVolumeContext{}, readDecodeError("get workload volumes", err)
	}
	return result, nil
}

package client

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"k8s.io/apimachinery/pkg/util/validation"
)

type VolumeContextReader interface {
	PodVolumes(context.Context, string, string, string) (api.PodVolumeContext, error)
}

// expectedUID binds an asynchronous result to a selected live Pod instance.
// Empty is valid for a direct lookup that has not selected an instance yet.
func (c *KubernetesAPIClient) PodVolumes(ctx context.Context, namespace, podName, expectedUID string) (api.PodVolumeContext, error) {
	var result api.PodVolumeContext
	if len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(podName)) != 0 {
		return result, fmt.Errorf("valid Pod and namespace names are required")
	}
	if !c.scope.allowsNamespace(namespace) {
		return result, fmt.Errorf("Pod volume namespace is outside the configured scope")
	}
	path := "/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(podName) + "/volumes"
	if err := c.getBounded(ctx, "get Pod volumes", path, &result, volumecontext.MaxPageBytes); err != nil {
		return result, err
	}
	if result.APIVersion != api.MemoryAPIGroup+"/"+api.MemoryAPIVersion || result.Kind != "PodVolumeContext" || result.Namespace != namespace || result.Name != podName || result.UID == "" || len(result.UID) > volumecontext.MaxUIDBytes ||
		(expectedUID != "" && string(result.UID) != expectedUID) || result.Context.SchemaVersion != volumecontext.SchemaVersion || result.Context.Namespace != namespace || result.Context.PodName != podName || len(result.Context.Volumes) > volumecontext.MaxVolumesPerPod {
		return api.PodVolumeContext{}, readDecodeError("get Pod volumes", fmt.Errorf("volume response does not match the selected Pod"))
	}
	if err := volumecontext.ValidateView(result.Context, time.Now().UTC()); err != nil {
		return api.PodVolumeContext{}, readDecodeError("get Pod volumes", err)
	}
	return result, nil
}

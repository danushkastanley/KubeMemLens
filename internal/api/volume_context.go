package api

import (
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"slices"
)

// PodVolumeContext carries the authorised Pod UID in resource metadata so live
// clients can reject a replaced selection. Default captures use Redacted data.
type PodVolumeContext struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Context           volumecontext.View `json:"context"`
}

func PodVolumeContextForSchema(value PodVolumeContext, schema int) PodVolumeContext {
	if schema >= VolumeHealthSnapshotSchemaVersion {
		return value
	}
	value.Context.Volumes = slices.Clone(value.Context.Volumes)
	for i := range value.Context.Volumes {
		value.Context.Volumes[i].Health = slices.Clone(value.Context.Volumes[i].Health)
		for j := range value.Context.Volumes[i].Health {
			value.Context.Volumes[i].Health[j].LastGood = nil
		}
	}
	return value
}

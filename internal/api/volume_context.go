package api

import (
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodVolumeContext carries the authorised Pod UID in resource metadata so live
// clients can reject a replaced selection. Default captures use Redacted data.
type PodVolumeContext struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Context           volumecontext.View `json:"context"`
}

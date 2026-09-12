package api

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkloadVolumeContext is one authorised server composition. PodVolumes follow
// live controller ownership; Workload.Pods contains only matching cgroup
// instances and may have lower coverage. Groups never sum filesystem values.
type WorkloadVolumeContext struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	ObservedAt        time.Time                       `json:"observedAt"`
	Workload          WorkloadSnapshot                `json:"workload"`
	PodVolumes        []PodVolumeContext              `json:"podVolumes"`
	Filesystems       []volumecontext.FilesystemGroup `json:"filesystems"`
	UnscheduledPods   []WorkloadUnscheduledPod        `json:"unscheduledPods,omitempty"`
}

type WorkloadUnscheduledPod struct {
	metav1.ObjectMeta `json:"metadata"`
	Reason            string `json:"reason"`
}

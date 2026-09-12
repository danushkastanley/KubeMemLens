package explain

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

type WorkloadVolumePodResult struct {
	PodIndex        int          `json:"podIndex"`
	MemoryAvailable bool         `json:"memoryAvailable"`
	Analysis        VolumeResult `json:"analysis"`
}

type WorkloadVolumeResult struct {
	MemoryObserved  int                       `json:"memoryObserved"`
	LivePods        int                       `json:"livePods"`
	MemorySeverity  Severity                  `json:"memorySeverity"`
	StorageSeverity Severity                  `json:"storageSeverity"`
	Pods            []WorkloadVolumePodResult `json:"pods"`
	Caveats         []string                  `json:"caveats"`
}

// Analyse only the server composition; consumers do not reconstruct controller
// membership or shared claims from independent API reads.
func AnalyzeWorkloadVolumes(value api.WorkloadVolumeContext, now time.Time) WorkloadVolumeResult {
	r := WorkloadVolumeResult{StorageSeverity: SeverityInfo}
	if api.ValidateWorkloadVolumeContext(value, now) != nil {
		r.Caveats = []string{"Workload volume scope is invalid or unavailable."}
		return r
	}
	r.MemoryObserved = len(value.Workload.Pods)
	r.LivePods = len(value.PodVolumes) + len(value.UnscheduledPods)
	r.MemorySeverity = AnalyzeWorkload(value.Workload).Severity
	memories := map[string]api.PodSnapshot{}
	for _, pod := range value.Workload.Pods {
		memories[pod.PodUID] = pod
	}
	for i, volumes := range value.PodVolumes {
		pod, available := memories[string(volumes.UID)]
		if !available {
			pod = api.PodSnapshot{Namespace: volumes.Namespace, PodName: volumes.Name, PodUID: string(volumes.UID)}
		}
		analysis := AnalyzeVolumes(VolumeInput{Pod: pod, Volumes: volumes.Context, Now: now})
		if analysis.StorageSeverity == SeverityHigh {
			r.StorageSeverity = SeverityHigh
		}
		r.Pods = append(r.Pods, WorkloadVolumePodResult{PodIndex: i, MemoryAvailable: available, Analysis: analysis})
	}
	r.Caveats = []string{"Shared PVC filesystem evidence is shown once with its per-Pod mounts. Filesystem values are not summed.", "Controller membership is observed during a bounded API read, not an atomic cluster snapshot.", "Correlation does not prove causality; container I/O pressure cannot identify a particular volume."}
	if r.MemoryObserved != r.LivePods || r.MemoryObserved == 0 {
		r.Caveats = append(r.Caveats, "Cgroup memory coverage is incomplete; missing Pods are not measured zero.")
	}
	if len(value.UnscheduledPods) > 0 {
		r.Caveats = append(r.Caveats, "Unscheduled Pods have no Node-bound filesystem or cgroup memory observation; other Pod evidence remains available.")
	}
	return r
}

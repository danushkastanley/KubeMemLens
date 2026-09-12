package volumeview

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
)

func WorkloadLines(value api.WorkloadVolumeContext, result explain.WorkloadVolumeResult, now time.Time, width int) []string {
	if api.ValidateWorkloadVolumeContext(value, now) != nil {
		return nodeview.Wrap([]string{"Workload volume context is unavailable; refresh the authorised read."}, width)
	}
	views := make([]volumecontext.View, len(value.PodVolumes))
	for i, pod := range value.PodVolumes {
		var err error
		views[i], err = volumecontext.AgeView(pod.Context, now)
		if err != nil {
			return nodeview.Wrap([]string{"Workload volume context is invalid or expired."}, width)
		}
	}
	lines := []string{fmt.Sprintf("Workload volumes: %s/%s/%s", value.Workload.Kind, value.Namespace, value.Name), "Membership observed: " + sample(value.ObservedAt, now), fmt.Sprintf("Cgroup memory coverage: %d/%d live Pods", result.MemoryObserved, result.LivePods), "Storage severity: " + string(result.StorageSeverity)}
	if result.MemoryObserved > 0 {
		lines = append(lines, "Observed memory charge: "+model.FormatCompactBytes(value.Workload.Memory.TotalBytes)+" | memory severity: "+string(result.MemorySeverity))
	} else {
		lines = append(lines, "Memory charge: unreported")
	}
	lines = append(lines, fmt.Sprintf("Filesystem groups: %d (values are not summed)", len(value.Filesystems)))
	for _, pod := range value.UnscheduledPods {
		lines = append(lines, "Pod "+pod.Name+": not scheduled at the Pod inventory read; Node-bound evidence is unreported.")
	}
	for index, group := range value.Filesystems {
		member := group.Selected
		pod := value.PodVolumes[member.Pod]
		volume := views[member.Pod].Volumes[member.Volume]
		label := fmt.Sprintf("Filesystem group %d", index+1)
		if volume.PVCName != "" {
			label += " | PVC: " + volume.PVCName
		}
		lines = append(lines, "", label, "Selected observation: "+pod.Name+"/"+volume.VolumeName)
		if group.ObservationsDiffer {
			lines = append(lines, "Source observations differ; the latest available observation is shown.")
		}
		lines = append(lines, fmt.Sprintf("Usage source: %s | %s | %s | %s", volume.Usage.Source, volume.Usage.Availability, volume.Usage.Freshness, volume.Usage.Reason))
		lines = append(lines, filesystemLines("Filesystem", volume.Usage.Filesystem, now)...)
		if volume.Usage.LastGood != nil {
			lines = append(lines, filesystemLines("Historical filesystem", volume.Usage.LastGood, now)...)
		}
		for _, ref := range group.Members {
			p := value.PodVolumes[ref.Pod]
			v := views[ref.Pod].Volumes[ref.Volume]
			medium := ""
			if v.Configuration.MemoryBacked {
				medium = "; memory-backed"
			}
			lines = append(lines, fmt.Sprintf("Pod %s | volume %s | %s%s | mounts %d (%d read-only)", p.Name, v.VolumeName, v.Configuration.Kind, medium, v.Configuration.MountCount, v.Configuration.ReadOnlyMountCount))
		}
	}
	for _, podResult := range result.Pods {
		pod := value.PodVolumes[podResult.PodIndex]
		lines = append(lines, "", "Pod evidence: "+pod.Name)
		if !podResult.MemoryAvailable {
			lines = append(lines, "Memory: unreported for this live Pod instance.")
		}
		for _, volume := range views[podResult.PodIndex].Volumes {
			lines = append(lines, "Volume health: "+volume.VolumeName)
			for _, health := range volume.Health {
				lines = append(lines, healthLines(health.HealthReport, false, now)...)
				if health.LastGood != nil {
					lines = append(lines, healthLines(*health.LastGood, true, now)...)
				}
			}
		}
		for _, signal := range podResult.Analysis.Signals {
			lines = append(lines, "- "+signal.Summary)
		}
		for _, caveat := range podResult.Analysis.Caveats {
			lines = append(lines, "- "+caveat)
		}
	}
	for _, caveat := range result.Caveats {
		lines = append(lines, "- "+caveat)
	}
	return nodeview.Wrap(lines, width)
}

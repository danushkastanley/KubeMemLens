package explain

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// containerNearestLimit returns the container using the largest share of its
// own memory.max, among containers already at limit risk. The kernel enforces
// each container's limit separately, so summed Pod usage can hide it.
func containerNearestLimit(containers []api.ContainerSnapshot) (api.ContainerSnapshot, bool) {
	var nearest api.ContainerSnapshot
	found := false
	for _, container := range containers {
		if !container.Memory.HasLimitRisk() {
			continue
		}
		if !found || container.Memory.LimitUsageRatio() > nearest.Memory.LimitUsageRatio() {
			nearest, found = container, true
		}
	}
	return nearest, found
}

func containerLimitRisk(container api.ContainerSnapshot, pod model.MemoryBreakdown) Result {
	memory := container.Memory
	headroom := memory.MaxBytes - minUint64(memory.TotalBytes, memory.MaxBytes)
	signals := []string{
		fmt.Sprintf("container %s is at %.0f%% of its memory.max", container.ContainerName, memory.LimitUsageRatio()*100),
		fmt.Sprintf("remaining container headroom is %s", model.FormatCompactBytes(headroom)),
	}
	if pod.MaxKnown && !pod.MaxUnlimited {
		signals = append(signals, fmt.Sprintf("Pod usage is %.0f%% of the summed container limits", pod.LimitUsageRatio()*100))
	}
	if memory.PeakKnown {
		signals = append(signals, fmt.Sprintf("container memory.peak=%s", model.FormatCompactBytes(memory.PeakBytes)))
	}
	return Result{
		Diagnosis: DiagnosisLimitRisk,
		Summary:   fmt.Sprintf("Container %s is close to its memory limit.", container.ContainerName),
		LikelyExplanation: "The Pod as a whole has headroom, but the kernel enforces each container's memory.max separately. " +
			"A transient allocation or cache burst in this container could cross its limit before the next sample.",
		SuggestedChecks: []string{
			"Compare this container's memory.peak with its limit and recent traffic peaks.",
			"Check container restarts and the last termination reason.",
			"Investigate the container's dominant memory bucket before changing its limit.",
		},
		Signals: signals,
	}
}

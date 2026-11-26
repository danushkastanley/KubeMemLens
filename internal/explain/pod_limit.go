package explain

import (
	"fmt"
	"math"

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

// ContainerLimitUsage names a container and the share of its own memory limit
// it uses, as a whole percent rounded the way the tables round it.
type ContainerLimitUsage struct {
	ContainerName string
	Percent       int
}

// ClosestContainerToLimit returns the container using the largest share of its
// own finite memory limit, when that tells the reader more than the Pod-level
// figure: the Pod has no finite effective limit (api.PodSnapshot.EffectiveLimit),
// or the container's rounded percentage is higher than the Pod's. Containers
// whose limit is unknown or unlimited are never candidates. Equal shares are
// settled by container name so the output is deterministic.
func ClosestContainerToLimit(pod api.PodSnapshot) (ContainerLimitUsage, bool) {
	var closest api.ContainerSnapshot
	found := false
	for _, container := range pod.Containers {
		// A figure without a name cannot be attributed in any table.
		if container.ContainerName == "" || !finiteLimit(container.Memory) {
			continue
		}
		if !found || closerToLimit(container, closest) {
			closest, found = container, true
		}
	}
	if !found {
		return ContainerLimitUsage{}, false
	}
	usage := ContainerLimitUsage{ContainerName: closest.ContainerName, Percent: wholePercent(closest.Memory)}
	limit := pod.EffectiveLimit()
	if limit.Known && !limit.Unlimited && limit.LimitBytes > 0 && usage.Percent <= roundedPercent(limit.UsageRatio()) {
		return ContainerLimitUsage{}, false
	}
	return usage, true
}

func closerToLimit(candidate, current api.ContainerSnapshot) bool {
	candidateRatio, currentRatio := candidate.Memory.LimitUsageRatio(), current.Memory.LimitUsageRatio()
	if candidateRatio != currentRatio {
		return candidateRatio > currentRatio
	}
	return candidate.ContainerName < current.ContainerName
}

func finiteLimit(memory model.MemoryBreakdown) bool {
	return memory.MaxKnown && !memory.MaxUnlimited && memory.MaxBytes > 0
}

// wholePercent rounds half to even, which is what "%.0f" prints, so the
// comparison matches the percentages a reader sees.
func wholePercent(memory model.MemoryBreakdown) int {
	return roundedPercent(memory.LimitUsageRatio())
}

func roundedPercent(ratio float64) int {
	return int(math.RoundToEven(ratio * 100))
}

func containerLimitRisk(container api.ContainerSnapshot, pod api.PodSnapshot) Result {
	memory := container.Memory
	headroom := memory.MaxBytes - minUint64(memory.TotalBytes, memory.MaxBytes)
	signals := []string{
		fmt.Sprintf("container %s is at %.0f%% of its memory.max", container.ContainerName, memory.LimitUsageRatio()*100),
		fmt.Sprintf("remaining container headroom is %s", model.FormatCompactBytes(headroom)),
	}
	if signal, ok := podLimitSignal(pod); ok {
		signals = append(signals, signal)
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

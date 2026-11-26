package cli

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// limitState says what is known about a memory limit. A bare byte count
// cannot: zero would mean both "no limit" and "limit not read".
type limitState string

const (
	limitUnknown   limitState = "unknown"
	limitUnlimited limitState = "unlimited"
	limitFinite    limitState = "finite"
)

// memoryLimit returns the limit in bytes, which is zero unless the state is finite.
func memoryLimit(memory model.MemoryBreakdown) (uint64, limitState) {
	if !memory.MaxKnown {
		return 0, limitUnknown
	}
	if memory.MaxUnlimited {
		return 0, limitUnlimited
	}
	return memory.MaxBytes, limitFinite
}

// podLimitUsage is the Pod's limit percentage, followed by the container
// closest to its own limit when the Pod figure hides it.
func podLimitUsage(pod api.PodSnapshot) string {
	usage, ok := explain.ClosestContainerToLimit(pod)
	if !ok {
		return limitUsage(pod.Memory)
	}
	return withClosestContainer(limitUsage(pod.Memory), usage.ContainerName, usage.Percent)
}

// withClosestContainer keeps the Pod figure first so that the column still
// starts with the value it has always shown.
func withClosestContainer(limit, container string, percent int) string {
	return fmt.Sprintf("%s (%s %d%%)", limit, container, percent)
}

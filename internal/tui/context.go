package tui

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

func podContextLines(pod api.PodSnapshot) []string {
	context := pod.Context
	owner := "none reported"
	if context.OwnerKind != "" && context.OwnerName != "" {
		owner = context.OwnerKind + "/" + context.OwnerName
	}
	workload := "unresolved"
	if context.WorkloadKind != "" && context.WorkloadName != "" {
		workload = context.WorkloadKind + "/" + context.WorkloadName
	}
	lines := []string{
		"Kubernetes context:",
		"Phase:                 " + tuiValue(context.Phase),
		"QoS:                   " + tuiValue(context.QoSClass),
		"Node MemoryPressure:   " + tuiValue(context.NodeMemoryPressure),
		"Node allocatable:      " + tuiKnownBytes(context.NodeMemoryAllocatable, context.NodeMemoryAllocatableKnown),
		"Runtime class:         " + tuiValue(context.RuntimeClassName),
		fmt.Sprintf("Memory emptyDir:       %d (%d limited)", context.MemoryEmptyDirCount, context.MemoryEmptyDirLimited),
		"Direct owner:          " + owner,
		"Workload:              " + workload,
		fmt.Sprintf("Restarts:              %d", context.RestartCount),
		"Container requests:    " + tuiResourceCoverage(context.MemoryRequestBytes, context.MemoryRequestContainers, len(pod.Containers)),
		"Container limits:      " + tuiResourceCoverage(context.MemoryLimitBytes, context.MemoryLimitContainers, len(pod.Containers)),
	}
	if !context.CreatedAt.IsZero() {
		lines = append(lines, "Created:               "+context.CreatedAt.UTC().Format(time.RFC3339))
	}
	if context.LastTerminationKnown {
		lines = append(lines, fmt.Sprintf("Last termination:      %s (exit %d)", tuiValue(context.LastTerminationReason), context.LastTerminationExitCode))
	}
	return lines
}

func tuiKnownBytes(bytes uint64, known bool) string {
	if !known {
		return "unknown"
	}
	return memmodel.FormatCompactBytes(bytes)
}

func memorySignalLines(memory memmodel.MemoryBreakdown) []string {
	lines := []string{}
	if memory.PeakKnown {
		lines = append(lines, "Peak:                 "+memmodel.FormatBytes(memory.PeakBytes))
	}
	if memory.MaxKnown {
		limit := "unlimited"
		if !memory.MaxUnlimited {
			limit = fmt.Sprintf("%s (%.0f%% used)", memmodel.FormatBytes(memory.MaxBytes), memory.LimitUsageRatio()*100)
		}
		lines = append(lines, "Hard limit:           "+limit)
	}
	if memory.SwapCurrentKnown {
		lines = append(lines, "Swap:                 "+memmodel.FormatBytes(memory.SwapCurrentBytes))
	}
	if memory.PressureKnown {
		lines = append(lines, fmt.Sprintf("Memory PSI avg10:     some %.2f%% / full %.2f%%", memory.PSISomeAvg10, memory.PSIFullAvg10))
	}
	return lines
}

func tuiResourceCoverage(bytes uint64, covered, total int) string {
	if covered == 0 {
		return fmt.Sprintf("not set (0/%d)", total)
	}
	return fmt.Sprintf("%s (%d/%d)", memmodel.FormatCompactBytes(bytes), covered, total)
}

func tuiValue(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

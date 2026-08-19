package cli

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func printPodContext(w interface{ Write([]byte) (int, error) }, pod api.PodSnapshot) {
	context := pod.Context
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Kubernetes context:")
	fmt.Fprintf(w, "  phase: %s\n", valueOrUnknown(context.Phase))
	fmt.Fprintf(w, "  QoS: %s\n", valueOrUnknown(context.QoSClass))
	fmt.Fprintf(w, "  node MemoryPressure: %s\n", valueOrUnknown(context.NodeMemoryPressure))
	if context.NodeMemoryAllocatableKnown {
		fmt.Fprintf(w, "  node allocatable memory: %s\n", model.FormatCompactBytes(context.NodeMemoryAllocatable))
	} else {
		fmt.Fprintln(w, "  node allocatable memory: unknown")
	}
	fmt.Fprintf(w, "  runtime class: %s\n", valueOrUnknown(context.RuntimeClassName))
	if context.MemoryEmptyDirCount > 0 {
		fmt.Fprintf(w, "  memory-backed emptyDir: %d (%d limited, %d unbounded; known limits %s)\n",
			context.MemoryEmptyDirCount, context.MemoryEmptyDirLimited,
			context.MemoryEmptyDirCount-context.MemoryEmptyDirLimited,
			model.FormatCompactBytes(context.MemoryEmptyDirLimitBytes))
	} else {
		fmt.Fprintln(w, "  memory-backed emptyDir: none reported")
	}
	if context.OwnerKind != "" && context.OwnerName != "" {
		fmt.Fprintf(w, "  direct owner: %s/%s\n", context.OwnerKind, context.OwnerName)
	} else {
		fmt.Fprintln(w, "  direct owner: none reported")
	}
	if context.WorkloadKind != "" && context.WorkloadName != "" {
		fmt.Fprintf(w, "  workload: %s/%s\n", context.WorkloadKind, context.WorkloadName)
	} else {
		fmt.Fprintln(w, "  workload: unresolved")
	}
	if !context.CreatedAt.IsZero() {
		fmt.Fprintf(w, "  created: %s (%s ago)\n", context.CreatedAt.UTC().Format(time.RFC3339), compactAge(context.CreatedAt))
	}
	fmt.Fprintf(w, "  restarts: %d\n", context.RestartCount)
	if context.LastTerminationKnown {
		fmt.Fprintf(w, "  last termination: %s (exit %d", valueOrUnknown(context.LastTerminationReason), context.LastTerminationExitCode)
		if !context.LastTerminationFinishedAt.IsZero() {
			fmt.Fprintf(w, ", %s", context.LastTerminationFinishedAt.UTC().Format(time.RFC3339))
		}
		fmt.Fprintln(w, ")")
	}
	fmt.Fprintf(w, "  memory request: %s\n", resourceCoverage(context.MemoryRequestBytes, context.MemoryRequestContainers, len(pod.Containers), "without request"))
	fmt.Fprintf(w, "  memory limit: %s\n", resourceCoverage(context.MemoryLimitBytes, context.MemoryLimitContainers, len(pod.Containers), "without limit"))
}

func resourceCoverage(bytes uint64, covered int, total int, missingLabel string) string {
	if covered == 0 {
		return fmt.Sprintf("not set (%d/%d containers; %d %s)", covered, total, total, missingLabel)
	}
	missing := total - covered
	if missing < 0 {
		missing = 0
	}
	return fmt.Sprintf("%s (%d/%d containers; %d %s)", model.FormatCompactBytes(bytes), covered, total, missing, missingLabel)
}

func limitUsage(memory model.MemoryBreakdown) string {
	if !memory.MaxKnown {
		return "unknown"
	}
	if memory.MaxUnlimited {
		return "unlimited"
	}
	return fmt.Sprintf("%.0f%%", memory.LimitUsageRatio()*100)
}

func compactAge(value time.Time) string {
	age := time.Since(value)
	if age < 0 {
		age = 0
	}
	return age.Round(time.Second).String()
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

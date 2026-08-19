package tui

import (
	"fmt"
	"sort"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

func (m appModel) nodeDetailLines() []string {
	for _, node := range buildNodeViews(m.data.Nodes, m.data.Pods, "") {
		if node.name != m.detail.nodeName {
			continue
		}
		lines := []string{
			"Node: " + node.name,
			"Captured: " + node.capturedAt.Format("2006-01-02 15:04:05 MST") + " (" + FormatAge(node.capturedAt) + " ago)",
			"Freshness: " + nodePressureLabel(node),
			"",
			"Node context:",
			"MemoryPressure:  " + tuiValue(node.environment.MemoryPressureStatus),
			"Allocatable:     " + tuiKnownBytes(node.environment.MemoryAllocatableBytes, node.environment.MemoryAllocatableKnown),
			"Cgroup:          " + tuiValue(node.environment.CgroupVersion) + " / " + tuiValue(node.environment.CgroupDriver),
			"Runtimes:        " + joinValues(node.environment.ContainerRuntimes),
			fmt.Sprintf("Cgroup read errors: %d", node.environment.CgroupReadErrors),
			"",
			"Observed workload scope:",
			fmt.Sprintf("Pods:             %d", node.podCount),
			fmt.Sprintf("Containers:       %d", node.containerCount),
			"Summed Pod charge: " + memmodel.FormatBytes(node.memory.TotalBytes),
			"",
			"Important:",
			"Summed Pod cgroup charge is not total node memory and must not be compared as if it were node usage.",
			"Use Node MemoryPressure and allocatable context to decide whether evidence is node-wide.",
			"",
			"Next commands:",
			"kubectl describe node/" + node.name,
			"kubectl memlens top pods -A --field-selector spec.nodeName=" + node.name,
		}
		return lines
	}
	return []string{"Selected node is no longer reported by the collector."}
}

func (m appModel) namespaceDetailLines() []string {
	for _, namespace := range m.data.Namespaces {
		if namespace.Namespace != m.detail.namespace {
			continue
		}
		result := explain.Analyze(namespace.Memory)
		return append(memorySummaryLines("Namespace", namespace.Namespace, namespace.Memory, result),
			"", fmt.Sprintf("Pods: %d", namespace.PodCount),
			"Captured: "+namespace.CapturedAt.Format("2006-01-02 15:04:05 MST"),
			"", "Next command:", "kubectl memlens top pods -n "+namespace.Namespace)
	}
	return []string{"Selected namespace is no longer present in collector snapshots."}
}

func (m appModel) workloadDetailLines() []string {
	for _, workload := range m.data.Workloads {
		if workload.Namespace != m.detail.namespace || workload.Kind != m.detail.workloadKind || workload.Name != m.detail.name {
			continue
		}
		result := explain.AnalyzeWorkload(workload)
		lines := memorySummaryLines(workload.Kind, workload.Namespace+"/"+workload.Name, workload.Memory, result)
		lines = append(lines,
			"", fmt.Sprintf("Replicas observed: %d", workload.PodCount),
			"Captured: "+workload.CapturedAt.Format("2006-01-02 15:04:05 MST")+" ("+FormatAge(workload.CapturedAt)+" ago)",
			"Largest Pod: "+workload.LargestPodName+" ("+memmodel.FormatCompactBytes(workload.LargestPodBytes)+")",
			"", "Pods by charged memory:")
		pods := append([]api.PodSnapshot(nil), workload.Pods...)
		sort.SliceStable(pods, func(i, j int) bool { return pods[i].Memory.TotalBytes > pods[j].Memory.TotalBytes })
		for _, pod := range pods {
			lines = append(lines, fmt.Sprintf("- %-36s %10s  %s", pod.PodName, memmodel.FormatCompactBytes(pod.Memory.TotalBytes), explain.AnalyzePod(pod).Diagnosis))
		}
		lines = append(lines, "", "Next command:",
			"kubectl memlens explain workload "+workload.Kind+"/"+workload.Name+" -n "+workload.Namespace)
		return lines
	}
	return []string{"Selected workload is no longer present in collector snapshots."}
}

func memorySummaryLines(kind, name string, memory memmodel.MemoryBreakdown, result explain.Result) []string {
	presentation := presentMemory(memory, 28, false)
	return []string{
		kind + ": " + name,
		"",
		"Diagnosis: " + string(result.Diagnosis),
		"Severity: " + string(result.Severity),
		"Confidence: " + string(result.Confidence) + " — " + result.ConfidenceReason,
		"Likely explanation: " + result.LikelyExplanation,
		"",
		"Composition (A/F/S/O): " + presentation.composition,
		"Total:       " + memmodel.FormatBytes(memory.TotalBytes),
		"Anon:        " + memmodel.FormatBytes(memory.RSSBytes()),
		"File cache:  " + memmodel.FormatBytes(memory.CacheBytes()),
		"Shmem:       " + memmodel.FormatBytes(memory.ShmemBytes),
		"Other:       " + memmodel.FormatBytes(memory.ResidualBytes()),
		"Signal:      " + presentation.signal,
	}
}

func (m appModel) findPod(namespace, name string) (api.PodSnapshot, bool) {
	for _, pod := range m.data.Pods {
		if pod.Namespace == namespace && pod.PodName == name {
			return pod, true
		}
	}
	return api.PodSnapshot{}, false
}

func (m appModel) findContainer(namespace, podName, containerName string) (api.ContainerSnapshot, bool) {
	for _, container := range m.data.Containers {
		if container.Namespace == namespace && container.PodName == podName && container.ContainerName == containerName {
			return container, true
		}
	}
	return api.ContainerSnapshot{}, false
}

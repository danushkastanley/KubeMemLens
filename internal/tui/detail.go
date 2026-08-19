package tui

import (
	"fmt"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

func (m appModel) detailLines(width int) []string {
	switch m.detail.kind {
	case entityNode:
		return m.nodeDetailLines()
	case entityNamespace:
		return m.namespaceDetailLines()
	case entityWorkload:
		return m.workloadDetailLines()
	case entityContainer:
		return m.containerDetailLines(width)
	case entityPod:
		pod, ok := m.findPod(m.detail.namespace, m.detail.podName)
		if !ok {
			return []string{"Selected Pod is no longer present in collector snapshots."}
		}
		return m.podDetailLines(pod, width, "Recent Pod history")
	default:
		pod, ok := m.selectedPod()
		if !ok {
			return []string{"Selected entity is no longer present in collector snapshots."}
		}
		return m.podDetailLines(pod, width, "Recent Pod history")
	}
}

func (m appModel) podDetailLines(pod api.PodSnapshot, width int, historyTitle string) []string {
	result := explain.AnalyzePod(pod)
	risk := podRisk(pod, m.riskNow(), m.staleAfter())
	presentation := presentMemory(pod.Memory, minInt(32, maxInt(8, width-20)), false)
	lines := []string{
		"Pod: " + pod.PodName,
		"Namespace: " + pod.Namespace,
		"Node: " + pod.NodeName,
		"Created: " + formatPodCreatedAt(pod),
		"Sampled: " + pod.CapturedAt.Format("2006-01-02 15:04:05 MST") + " (" + FormatAge(pod.CapturedAt) + " ago)",
		"",
		"Diagnosis: " + string(result.Diagnosis),
		"Severity: " + string(result.Severity),
		"Confidence: " + string(result.Confidence) + " — " + result.ConfidenceReason,
		"Likely explanation: " + result.LikelyExplanation,
		"Risk order: " + risk.label + " — " + riskReasonsText(risk),
		"Observation window: " + result.EvidenceWindow.ObservationDescription(),
		"Counter window: " + result.EvidenceWindow.DeltaDescription(),
		"",
		"Memory composition (A/F/S/O): " + presentation.composition,
		"Total charged memory: " + memmodel.FormatBytes(pod.Memory.TotalBytes),
		"Limit:                " + presentation.limit,
		"Current signal:       " + presentation.signal,
		"RSS / anon:           " + memmodel.FormatBytes(pod.Memory.RSSBytes()),
		"File cache:           " + memmodel.FormatBytes(pod.Memory.CacheBytes()),
		"Shmem / tmpfs:        " + memmodel.FormatBytes(pod.Memory.ShmemBytes),
		"Residual / other:     " + memmodel.FormatBytes(pod.Memory.ResidualBytes()),
		"",
	}
	lines = append(lines, historyTitle+":")
	switch {
	case m.selectedHistory.loading:
		lines = append(lines, "Loading bounded history...")
	case m.selectedHistory.err != nil:
		age := "unknown age"
		if !m.selectedHistory.updatedAt.IsZero() {
			age = FormatAge(m.selectedHistory.updatedAt) + " ago"
		}
		lines = append(lines, "History refresh failed; showing last good data from "+age+": "+m.selectedHistory.err.Error())
		lines = append(lines, renderHistoryTrend(pod, m.selectedHistory.series, width)...)
	default:
		lines = append(lines, renderHistoryTrend(pod, m.selectedHistory.series, width)...)
	}
	lines = append(lines, "")
	lines = append(lines, podContextLines(pod)...)
	lines = append(lines,
		"",
		"Secondary detail (overlaps primary composition):",
		"Active file:          "+memmodel.FormatBytes(pod.Memory.ActiveFileBytes),
		"Inactive file:        "+memmodel.FormatBytes(pod.Memory.InactiveFileBytes),
		"Kernel total:         "+memmodel.FormatBytes(pod.Memory.KernelBytes),
		"Slab:                 "+memmodel.FormatBytes(pod.Memory.SlabBytes),
		"  reclaimable:        "+memmodel.FormatBytes(pod.Memory.SlabReclaimableBytes),
		"  unreclaimable:      "+memmodel.FormatBytes(pod.Memory.SlabUnreclaimableBytes),
		"Socket memory:        "+memmodel.FormatBytes(pod.Memory.SocketBytes),
		"Page tables:          "+memmodel.FormatBytes(pod.Memory.PageTableBytes),
		"Mapped file:          "+memmodel.FormatBytes(pod.Memory.FileMappedBytes),
		"THP anon/file/shmem:  "+memmodel.FormatBytes(pod.Memory.AnonTHPBytes)+" / "+memmodel.FormatBytes(pod.Memory.FileTHPBytes)+" / "+memmodel.FormatBytes(pod.Memory.ShmemTHPBytes),
		"Other kernel:         "+memmodel.FormatBytes(pod.Memory.KernelOtherBytes()),
		"Dirty/writeback:      "+memmodel.FormatDirtyWriteback(pod.Memory),
	)
	lines = append(lines, memorySignalLines(pod.Memory)...)
	lines = append(lines,
		fmt.Sprintf("OOM events:           %d", pod.Memory.OOMEvents),
		fmt.Sprintf("OOM kill events:      %d", pod.Memory.OOMKillEvents),
		fmt.Sprintf("High events:          %d", pod.Memory.HighEvents),
		fmt.Sprintf("Max events:           %d", pod.Memory.MaxEvents),
		"", "Signals:",
	)
	for _, signal := range result.Signals {
		lines = append(lines, "- "+signal)
	}
	lines = append(lines, "", "Suggested checks:")
	for _, check := range result.SuggestedChecks {
		lines = append(lines, "- "+check)
	}
	lines = append(lines, "", "Caveats:")
	for _, caveat := range result.Caveats {
		lines = append(lines, "- "+caveat)
	}
	lines = append(lines, "", "Containers:")
	lines = append(lines, renderContainerSummary(pod.Containers)...)
	lines = append(lines, "", "Next commands:",
		"kubectl memlens history pod "+pod.PodName+" -n "+pod.Namespace,
		"kubectl describe pod/"+pod.PodName+" -n "+pod.Namespace,
	)
	return lines
}

func (m appModel) containerDetailLines(width int) []string {
	container, ok := m.findContainer(m.detail.namespace, m.detail.podName, m.detail.containerName)
	if !ok {
		return []string{"Selected container is no longer present in collector snapshots."}
	}
	result := explain.AnalyzeContainer(container)
	presentation := presentMemory(container.Memory, minInt(32, maxInt(8, width-20)), false)
	lines := []string{
		"Container: " + container.ContainerName,
		"Pod: " + container.Namespace + "/" + container.PodName,
		"Node: " + container.NodeName,
		"Captured: " + container.CapturedAt.Format("2006-01-02 15:04:05 MST"),
		"",
		"Diagnosis: " + string(result.Diagnosis),
		"Severity: " + string(result.Severity),
		"Confidence: " + string(result.Confidence) + " — " + result.ConfidenceReason,
		"Likely explanation: " + result.LikelyExplanation,
		"",
		"Composition (A/F/S/O): " + presentation.composition,
		"Total:       " + memmodel.FormatBytes(container.Memory.TotalBytes),
		"Limit:       " + presentation.limit,
		"Signal:      " + presentation.signal,
		"Anon:        " + memmodel.FormatBytes(container.Memory.RSSBytes()),
		"File cache:  " + memmodel.FormatBytes(container.Memory.CacheBytes()),
		"Shmem:       " + memmodel.FormatBytes(container.Memory.ShmemBytes),
		"Other:       " + memmodel.FormatBytes(container.Memory.ResidualBytes()),
		"",
		"Container context:",
		fmt.Sprintf("Request:      %s", knownResource(container.Context.MemoryRequestBytes, container.Context.MemoryRequestKnown)),
		fmt.Sprintf("Limit:        %s", knownResource(container.Context.MemoryLimitBytes, container.Context.MemoryLimitKnown)),
		fmt.Sprintf("Restarts:     %d", container.Context.RestartCount),
		"QoS:          " + tuiValue(container.Context.QoSClass),
		"Workload:     " + strings.Trim(strings.Join([]string{container.Context.WorkloadKind, container.Context.WorkloadName}, "/"), "/"),
	}
	if pod, found := m.findPod(container.Namespace, container.PodName); found {
		lines = append(lines, "", "Parent-Pod history (container history is not retained):")
		lines = append(lines, renderHistoryTrend(pod, m.selectedHistory.series, width)...)
	}
	lines = append(lines, "", "Next commands:",
		"kubectl memlens explain pod "+container.PodName+" -n "+container.Namespace,
		"kubectl describe pod/"+container.PodName+" -n "+container.Namespace,
	)
	return lines
}

func renderContainerSummary(containers []api.ContainerSnapshot) []string {
	widths := []int{18, 8, 8, 8, 7, 8, 18}
	lines := []string{tableRow([]string{"CONTAINER", "TOTAL", "RSS", "CACHE", "SHMEM", "OTHER", "DIAGNOSIS"}, widths, nil)}
	for _, container := range containers {
		lines = append(lines, tableRow([]string{
			container.ContainerName,
			memmodel.FormatCompactBytes(container.Memory.TotalBytes),
			memmodel.FormatCompactBytes(container.Memory.RSSBytes()),
			memmodel.FormatCompactBytes(container.Memory.CacheBytes()),
			memmodel.FormatCompactBytes(container.Memory.ShmemBytes),
			memmodel.FormatCompactBytes(container.Memory.ResidualBytes()),
			string(explain.AnalyzeContainer(container).Diagnosis),
		}, widths, numericIndexes(1, 2, 3, 4, 5)))
	}
	return lines
}

func knownResource(bytes uint64, known bool) string {
	if !known {
		return "not set"
	}
	return memmodel.FormatBytes(bytes)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

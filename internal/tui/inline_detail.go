package tui

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

func (m appModel) inlineDetailLines(_ int) []string {
	switch m.view {
	case viewNodes:
		items := m.visibleNodes()
		selected := m.viewports[viewNodes].selected
		if selected >= 0 && selected < len(items) {
			item := items[selected]
			lines := summaryLines("Node", item.name, item.podCount, item.memory, item.capturedAt.String())
			return append(lines,
				"", "Pressure", nodePressureLabel(item),
				"Allocatable", tuiKnownBytes(item.environment.MemoryAllocatableBytes, item.environment.MemoryAllocatableKnown),
				"Pod charge is not total node memory.")
		}
	case viewNamespaces:
		items := m.visibleNamespaces()
		selected := m.viewports[viewNamespaces].selected
		if selected >= 0 && selected < len(items) {
			item := items[selected]
			return summaryLines("Namespace", item.Namespace, item.PodCount, item.Memory, item.CapturedAt.String())
		}
	case viewWorkloads:
		items := m.visibleWorkloads()
		selected := m.viewports[viewWorkloads].selected
		if selected >= 0 && selected < len(items) {
			item := items[selected]
			lines := summaryLines(item.Kind, item.Name, item.PodCount, item.Memory, item.CapturedAt.String())
			return append(lines, "", "Largest Pod", item.LargestPodName+"  "+memmodel.FormatCompactBytes(item.LargestPodBytes))
		}
	case viewPods:
		if pod, ok := m.selectedVisiblePod(); ok {
			lines := compactPodLines(pod)
			risk := podRisk(pod, m.riskNow(), m.staleAfter())
			return append(lines, "", "Risk order", risk.label+" — "+riskReasonsText(risk))
		}
	case viewContainers:
		items := m.visibleContainers()
		selected := m.viewports[viewContainers].selected
		if selected >= 0 && selected < len(items) {
			item := items[selected]
			result := explain.AnalyzeContainer(item)
			return []string{
				"Container", item.ContainerName,
				"Pod", item.Namespace + "/" + item.PodName,
				"Node", item.NodeName,
				"",
				"Total       " + memmodel.FormatCompactBytes(item.Memory.TotalBytes),
				"Anon        " + memmodel.FormatCompactBytes(item.Memory.RSSBytes()),
				"File cache  " + memmodel.FormatCompactBytes(item.Memory.CacheBytes()),
				"Shmem       " + memmodel.FormatCompactBytes(item.Memory.ShmemBytes),
				"Other       " + memmodel.FormatCompactBytes(item.Memory.ResidualBytes()),
				"",
				"Diagnosis", string(result.Diagnosis),
				"Severity    " + string(result.Severity),
				"Confidence  " + string(result.Confidence),
			}
		}
	}
	return []string{"No selected entity."}
}

func (m appModel) selectedVisiblePod() (api.PodSnapshot, bool) {
	items := m.visiblePods()
	selected := m.viewports[viewPods].selected
	if selected < 0 || selected >= len(items) {
		return api.PodSnapshot{}, false
	}
	return items[selected], true
}

func compactPodLines(pod api.PodSnapshot) []string {
	result := explain.AnalyzePod(pod)
	presentation := presentMemory(pod.Memory, 24, false)
	lines := []string{
		"Pod", pod.PodName,
		"Namespace", pod.Namespace,
		"Node", pod.NodeName,
		"",
		"Diagnosis", string(result.Diagnosis),
		"Severity    " + string(result.Severity),
		"Confidence  " + string(result.Confidence),
		"",
		"Total       " + memmodel.FormatCompactBytes(pod.Memory.TotalBytes),
		"Limit       " + presentation.limit,
		"Composition " + presentation.composition,
		"A/F/S/O     anon/file-cache/shmem/other",
		"Signal      " + presentation.signal,
		"Anon        " + memmodel.FormatCompactBytes(pod.Memory.RSSBytes()),
		"File cache  " + memmodel.FormatCompactBytes(pod.Memory.CacheBytes()),
		"Shmem       " + memmodel.FormatCompactBytes(pod.Memory.ShmemBytes),
		"Other       " + memmodel.FormatCompactBytes(pod.Memory.ResidualBytes()),
	}
	lines = append(lines, "", "Likely explanation", result.LikelyExplanation)
	return lines
}

func summaryLines(kind, name string, count int, memory memmodel.MemoryBreakdown, captured string) []string {
	result := explain.Analyze(memory)
	return []string{
		kind, name,
		"",
		fmt.Sprintf("Pods        %d", count),
		"Total       " + memmodel.FormatCompactBytes(memory.TotalBytes),
		"Anon        " + memmodel.FormatCompactBytes(memory.RSSBytes()),
		"File cache  " + memmodel.FormatCompactBytes(memory.CacheBytes()),
		"Shmem       " + memmodel.FormatCompactBytes(memory.ShmemBytes),
		"Other       " + memmodel.FormatCompactBytes(memory.ResidualBytes()),
		"",
		"Diagnosis", string(result.Diagnosis),
		"Captured", captured,
	}
}

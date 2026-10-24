package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

func (m appModel) podEvidenceLabel(pod api.PodSnapshot) string {
	freshness := string(pod.Freshness)
	if freshness == "" {
		freshness = "unknown"
	}
	if !pod.CapturedAt.IsZero() && time.Since(pod.CapturedAt) > m.staleAfter() {
		freshness = "stale"
	}
	completeness := string(pod.Completeness)
	if completeness == "" {
		completeness = "unknown completeness"
	}
	state := freshness + " / " + completeness + " · " + FormatAge(pod.CapturedAt) + " ago"
	if m.statusErr != nil {
		return "Connection error · last known · " + state
	}
	if m.paused {
		return "Paused · " + state
	}
	return state
}

func (m appModel) podPanelLines(pod api.PodSnapshot, panel podPanel, width int) []string {
	var lines []string
	switch panel {
	case podComposition:
		lines = podCompositionLines(pod, width)
	case podContext:
		lines = podDashboardContext(pod)
		lines = append(lines, "", "Evidence: "+m.podEvidenceLabel(pod), "Sample: "+pod.CapturedAt.UTC().Format(time.RFC3339), "", "Read-only inspection.")
	case podHistory:
		lines = m.podDashboardHistory(pod, width)
	case podChecks:
		finding := explain.AnalyzePod(pod)
		lines = []string{finding.Summary, "", "Confidence: " + string(finding.Confidence), finding.ConfidenceReason, ""}
		if len(finding.SuggestedChecks) > 0 {
			lines = append(lines, "Next check: "+finding.SuggestedChecks[0], "")
		}
		for index, label := range []string{"View suggested checks", "Compare with another pod", "Inspect all raw evidence"} {
			line := fmt.Sprintf("  %d %s", index+1, label)
			selected := m.podDashboard.actionIndex == index && m.podDashboard.panel == podChecks
			if selected {
				line = ">" + line[1:]
			}
			lines = append(lines, styleSelected(line, selected))
		}
		lines = append(lines, "", "Enter: open selected action", "No commands run automatically.", "", "Before acting:")
		lines = append(lines, finding.Caveats...)
	case podContainers:
		if !m.data.ContainersLoaded {
			return []string{"Loading container evidence..."}
		}
		if m.containerErr != nil {
			return []string{"Container refresh failed. Press r to retry."}
		}
		lines = []string{tableRow([]string{"NAME", "CHARGE", "ANON", "CACHE", "SHMEM"}, []int{maxInt(10, width-42), 8, 8, 8, 7}, nil)}
		for index, container := range pod.Containers {
			selected := index == m.podDashboard.containerIndex && m.podDashboard.panel == podContainers
			marker := " "
			if selected {
				marker = ">"
			}
			line := marker + tableRow([]string{container.ContainerName, memmodel.FormatCompactBytes(container.Memory.TotalBytes), memmodel.FormatCompactBytes(container.Memory.RSSBytes()), memmodel.FormatCompactBytes(container.Memory.CacheBytes()), memmodel.FormatCompactBytes(container.Memory.ShmemBytes)}, []int{maxInt(9, width-43), 8, 8, 8, 7}, numericIndexes(1, 2, 3, 4))
			lines = append(lines, styleSelected(line, selected))
		}
		lines = append(lines, "", "Enter: inspect selected container")
	case podSignals:
		lines = podDashboardSignals(pod)
	}
	return strings.Split(ansi.Wrap(strings.Join(lines, "\n"), maxInt(1, width), ""), "\n")
}

func podDashboardContext(pod api.PodSnapshot) []string {
	owner := strings.TrimSpace(pod.Context.OwnerKind + " " + pod.Context.OwnerName)
	if owner == "" {
		owner = "not reported"
	}
	return []string{
		"Namespace: " + pod.Namespace, "Pod: " + pod.PodName,
		"Phase: " + tuiValue(pod.Context.Phase), "QoS: " + tuiValue(pod.Context.QoSClass),
		fmt.Sprintf("Containers: %d · restarts: %d", len(pod.Containers), pod.Context.RestartCount),
		"Owner: " + owner, "Node: " + pod.NodeName,
		"Request: " + tuiResourceCoverage(pod.Context.MemoryRequestBytes, pod.Context.MemoryRequestContainers, len(pod.Containers)),
		"Limit: " + podLimit(pod), "Node MemoryPressure: " + tuiValue(pod.Context.NodeMemoryPressure),
	}
}

func podCompositionLines(pod api.PodSnapshot, width int) []string {
	memory := pod.Memory
	lines := []string{compositionBar(memory, maxInt(4, width), false), "", "COMPONENT           CHARGE     SHARE"}
	values := []uint64{memory.RSSBytes(), memory.CacheBytes(), memory.ShmemBytes, memory.ResidualBytes()}
	for index, label := range []string{"A Anonymous", "F File cache", "S Shmem / tmpfs", "O Residual"} {
		share := "n/a"
		if memory.TotalBytes > 0 {
			percentage := float64(values[index]) * 100 / float64(memory.TotalBytes)
			share = fmt.Sprintf("%.0f%%", percentage)
			if values[index] > 0 && percentage < 1 {
				share = "<1%"
			}
		}
		lines = append(lines, fmt.Sprintf("%-17s %10s %6s", label, memmodel.FormatCompactBytes(values[index]), share))
	}
	lines = append(lines, "", explain.AnalyzePod(pod).Summary, "Kernel details overlap; press e for all evidence.")
	return lines
}

func podDashboardSignals(pod api.PodSnapshot) []string {
	window := explain.AnalyzePod(pod).EvidenceWindow
	lines := []string{"Counters: " + window.DeltaDescription()}
	memory := pod.Memory
	if !memory.EventDeltasKnown || !window.DeltaKnown || !window.DeltaComplete {
		lines = append(lines, "Event increments unavailable / incomplete.")
	} else {
		oom, kill, high, maxEvents := memory.RecentEventCounts()
		lines = append(lines, fmt.Sprintf("OOM %d · kills %d · high %d · max %d", oom, kill, high, maxEvents))
	}
	if memory.PressureKnown {
		lines = append(lines, fmt.Sprintf("PSI some/full: %.2f%% / %.2f%%", memory.PSISomeAvg10, memory.PSIFullAvg10))
	} else {
		lines = append(lines, "PSI: unavailable")
	}
	return lines
}

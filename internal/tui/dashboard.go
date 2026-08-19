package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/danushkastanley/kube-memlens/internal/api"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

const dashboardGap = 1

func (m appModel) renderWideDashboard(plan layoutPlan) string {
	summaryHeight := 6
	lowerHeight := maxInt(9, plan.bodyRows/4)
	mainHeight := plan.bodyRows - summaryHeight - lowerHeight - dashboardGap*2
	if mainHeight < 12 {
		mainHeight = 12
		lowerHeight = maxInt(6, plan.bodyRows-summaryHeight-mainHeight-dashboardGap*2)
	}

	namespaceWidth := maxInt(27, plan.width/6)
	detailWidth := maxInt(39, plan.width/4)
	tableWidth := plan.width - namespaceWidth - detailWidth - dashboardGap*2
	if tableWidth < 72 {
		tableWidth = 72
		detailWidth = plan.width - namespaceWidth - tableWidth - dashboardGap*2
	}

	summary := m.renderDashboardSummary(plan.width, summaryHeight)
	main := lipgloss.JoinHorizontal(lipgloss.Top,
		m.dashboardPanel("NAMESPACES", m.dashboardNamespaces(namespaceWidth-2, mainHeight-3), namespaceWidth, mainHeight, false),
		strings.Repeat(" ", dashboardGap),
		m.dashboardPanel("PODS · RISK ORDER", m.dashboardPods(tableWidth-2, mainHeight-3), tableWidth, mainHeight, m.focus == focusTable),
		strings.Repeat(" ", dashboardGap),
		m.dashboardPanel("POD DETAILS", m.dashboardPodDetail(detailWidth-2, mainHeight-3), detailWidth, mainHeight, m.focus == focusDetail),
	)

	leftWidth := plan.width / 2
	rightWidth := plan.width - leftWidth - dashboardGap
	lower := lipgloss.JoinHorizontal(lipgloss.Top,
		m.dashboardPanel("NODE MEMORY CONTEXT", m.dashboardNodes(leftWidth-2, lowerHeight-3), leftWidth, lowerHeight, false),
		strings.Repeat(" ", dashboardGap),
		m.dashboardPanel("CURRENT CGROUP SIGNALS", m.dashboardSignals(rightWidth-2, lowerHeight-3), rightWidth, lowerHeight, false),
	)
	return lipgloss.JoinVertical(lipgloss.Left, summary, main, lower)
}

func (m appModel) renderDashboardSummary(width, height int) string {
	titles := []string{"OBSERVED POD CHARGE", "NODE ALLOCATABLE", "CHARGE / ALLOCATABLE", "TOP NAMESPACE", "RISK PODS"}
	values := m.dashboardSummaryValues()
	baseWidth := (width - (len(titles)-1)*dashboardGap) / len(titles)
	remainder := width - baseWidth*len(titles) - (len(titles)-1)*dashboardGap
	panels := make([]string, 0, len(titles)*2-1)
	for index, title := range titles {
		panelWidth := baseWidth
		if index < remainder {
			panelWidth++
		}
		content := currentTheme().accent.Render(title) + "\n" + values[index]
		panels = append(panels, m.dashboardPanel("", content, panelWidth, height, false))
		if index < len(titles)-1 {
			panels = append(panels, strings.Repeat(" ", dashboardGap))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, panels...)
}

func (m appModel) dashboardSummaryValues() []string {
	var charge, allocatable uint64
	knownNodes := 0
	for _, pod := range m.data.Pods {
		charge += pod.Memory.TotalBytes
	}
	for _, node := range m.data.Nodes {
		if node.Environment.MemoryAllocatableKnown {
			allocatable += node.Environment.MemoryAllocatableBytes
			knownNodes++
		}
	}
	ratio := "unknown"
	if allocatable > 0 {
		ratio = fmt.Sprintf("%.1f%%\npartial scope", float64(charge)*100/float64(allocatable))
	}
	topName, topBytes := "none", uint64(0)
	for _, namespace := range m.data.Namespaces {
		if namespace.Memory.TotalBytes > topBytes {
			topName, topBytes = namespace.Namespace, namespace.Memory.TotalBytes
		}
	}
	riskCount := 0
	for _, pod := range m.data.Pods {
		label := podRisk(pod, m.riskNow(), m.staleAfter()).label
		if label == "CRIT" || label == "HIGH" || label == "STALE" {
			riskCount++
		}
	}
	known := fmt.Sprintf("%d/%d nodes", knownNodes, len(m.data.Nodes))
	return []string{
		memmodel.FormatCompactBytes(charge) + "\nPod cgroups only",
		memmodel.FormatCompactBytes(allocatable) + "\n" + known,
		ratio,
		topName + "\n" + memmodel.FormatCompactBytes(topBytes),
		styleRisk(fmt.Sprintf("%d", riskCount)) + "\ncritical/high/stale",
	}
}

func (m appModel) dashboardNamespaces(width, rows int) string {
	items := append([]api.NamespaceSnapshot(nil), m.data.Namespaces...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Memory.TotalBytes > items[j].Memory.TotalBytes })
	lines := []string{tableRow([]string{"NAMESPACE", "PODS", "CHARGE"}, []int{maxInt(8, width-15), 4, 7}, numericIndexes(1, 2))}
	selectedPod, selected := m.selectedVisiblePod()
	for _, item := range items {
		if len(lines) >= rows {
			break
		}
		marker := " "
		if selected && item.Namespace == selectedPod.Namespace {
			marker = "›"
		}
		line := marker + tableRow([]string{item.Namespace, fmt.Sprintf("%d", item.PodCount), memmodel.FormatCompactBytes(item.Memory.TotalBytes)}, []int{maxInt(7, width-16), 4, 7}, numericIndexes(1, 2))
		lines = append(lines, styleSelected(line, marker == "›"))
	}
	return strings.Join(lines, "\n")
}

func (m appModel) dashboardPods(width, rows int) string {
	items := m.visiblePods()
	if len(items) == 0 {
		return "No Pods match the current filter."
	}
	widths := dashboardPodWidths(width)
	lines := []string{tableRow([]string{"POD", "NODE", "TOTAL", "LIMIT", "A/F/S/O", "SIGNAL", "RISK", "AGE"}, widths, nil)}
	viewport := m.viewports[viewPods]
	viewport.resize(maxInt(1, rows-1))
	viewport.reconcile(len(items))
	start, end := viewport.visibleRange()
	for index := start; index < end; index++ {
		pod := items[index]
		presentation := presentMemory(pod.Memory, widths[4], false)
		risk := podRisk(pod, m.riskNow(), m.staleAfter())
		row := tableRow([]string{
			pod.PodName, pod.NodeName, memmodel.FormatCompactBytes(pod.Memory.TotalBytes),
			presentation.limit, presentation.composition, presentation.signal,
			risk.label + trendLabel(m.podTrends[podKey(pod.Namespace, pod.PodName)]), FormatAge(pod.CapturedAt),
		}, widths, numericIndexes(2))
		selected := index == viewport.selected
		marker := " "
		if selected {
			marker = "›"
		}
		lines = append(lines, styleSelected(marker+row, selected))
	}
	return strings.Join(lines, "\n")
}

func dashboardPodWidths(width int) []int {
	fixed := []int{12, 8, 13, 10, 12, 6, 4}
	used := 1 + 2*len(fixed)
	for _, cellWidth := range fixed {
		used += cellWidth
	}
	nameWidth := maxInt(14, width-used)
	return []int{nameWidth, fixed[0], fixed[1], fixed[2], fixed[3], fixed[4], fixed[5], fixed[6]}
}

func (m appModel) dashboardPodDetail(width, rows int) string {
	pod, ok := m.selectedVisiblePod()
	if !ok {
		return "No selected Pod."
	}
	lines := compactPodLines(pod)
	risk := podRisk(pod, m.riskNow(), m.staleAfter())
	lines = append(lines, "", "Risk order", risk.label+" — "+riskReasonsText(risk))
	viewport := m.viewports[viewDetail]
	viewport.resize(rows)
	viewport.reconcile(len(lines))
	return truncateLines(viewportWindow(viewport, lines), width)
}

func (m appModel) dashboardNodes(width, rows int) string {
	items := buildNodeViews(m.data.Nodes, m.data.Pods, "")
	lines := []string{tableRow([]string{"NODE", "PODS", "OBSERVED CHARGE", "ALLOCATABLE", "PRESSURE"}, []int{maxInt(12, width-43), 4, 13, 11, 8}, numericIndexes(1, 2, 3))}
	for _, node := range items {
		if len(lines) >= rows {
			break
		}
		allocatable := "unknown"
		if node.environment.MemoryAllocatableKnown {
			allocatable = memmodel.FormatCompactBytes(node.environment.MemoryAllocatableBytes)
		}
		lines = append(lines, tableRow([]string{node.name, fmt.Sprintf("%d", node.podCount), memmodel.FormatCompactBytes(node.memory.TotalBytes), allocatable, nodePressureLabel(node)}, []int{maxInt(12, width-43), 4, 13, 11, 8}, numericIndexes(1, 2, 3)))
	}
	return strings.Join(lines, "\n")
}

func (m appModel) dashboardSignals(width, rows int) string {
	type signal struct {
		pod   api.PodSnapshot
		label string
		score int
	}
	items := make([]signal, 0, len(m.data.Pods))
	for _, pod := range m.data.Pods {
		label := incidentLabel(pod.Memory)
		if label == "clear" || label == "baseline" {
			continue
		}
		risk := podRisk(pod, m.riskNow(), m.staleAfter())
		items = append(items, signal{pod: pod, label: label, score: risk.score})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].score > items[j].score })
	lines := []string{tableRow([]string{"AGE", "NAMESPACE", "POD", "SAMPLED EVIDENCE"}, []int{5, 14, maxInt(14, width-43), 18}, nil)}
	for _, item := range items {
		if len(lines) >= rows {
			break
		}
		lines = append(lines, tableRow([]string{FormatAge(item.pod.CapturedAt), item.pod.Namespace, item.pod.PodName, item.label}, []int{5, 14, maxInt(14, width-43), 18}, nil))
	}
	if len(lines) == 1 {
		lines = append(lines, currentTheme().healthy.Render("No OOM, memory.high, memory.max, or PSI signal in the current sample."))
	}
	return strings.Join(lines, "\n")
}

func (m appModel) dashboardPanel(title, content string, width, height int, focused bool) string {
	if width < 3 {
		width = 3
	}
	if height < 3 {
		height = 3
	}
	if title != "" {
		content = currentTheme().accent.Render(title) + "\n" + content
	}
	border := currentTheme().border
	if focused {
		border = currentTheme().focus
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(border).
		Width(width - 2).
		Height(height - 2).
		MaxWidth(width - 2).
		MaxHeight(height - 2).
		Render(content)
}

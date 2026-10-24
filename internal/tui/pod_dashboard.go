package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

type podPanelBox struct{ width, height int }

func (m appModel) podDashboardWide() bool { return m.width >= 150 && m.height >= 40 }

func (m appModel) podDashboardBoxes() [podPanelCount]podPanelBox {
	w, rows := m.layout().width, m.layout().bodyRows
	if !m.podDashboardWide() {
		var boxes [podPanelCount]podPanelBox
		for i := range boxes {
			boxes[i] = podPanelBox{w, maxInt(4, rows-2)}
		}
		return boxes
	}
	bottom := maxInt(7, rows/5)
	middle := rows - 5 - bottom - 2
	left, right := w/4, w*3/10
	centre := w - left - right - 2
	upper := middle / 2
	return [podPanelCount]podPanelBox{
		{centre, upper}, {left, middle}, {centre, middle - upper},
		{right, middle}, {w / 2, bottom}, {w - w/2 - 1, bottom},
	}
}

func (m appModel) renderPodDashboard() string {
	pod, ok := m.findPod(m.detail.namespace, m.detail.podName)
	if !ok {
		return "Selected Pod is no longer present. Esc returns to the previous dashboard."
	}
	boxes := m.podDashboardBoxes()
	panel := func(id podPanel) string {
		box := boxes[id]
		lines := m.podPanelLines(pod, id, box.width-2)
		v := m.podDashboard.panels[id]
		v.resize(maxInt(1, box.height-4))
		v.reconcile(len(lines))
		start, end := v.visibleRange()
		content := strings.Join(lines[start:end], "\n")
		if len(lines) > v.capacity {
			content += "\n" + styleHelp(fmt.Sprintf("%d-%d/%d · j/k scroll", start+1, end, len(lines)))
		}
		title := podPanelTitles[id]
		if m.podDashboard.panel == id {
			title = "> " + title
		}
		return m.dashboardPanel(title, content, box.width, box.height, m.podDashboard.panel == id)
	}
	if !m.podDashboardWide() {
		summary := fmt.Sprintf("%s · limit %s · %s", memmodel.FormatCompactBytes(pod.Memory.TotalBytes), podLimit(pod), explain.AnalyzePod(pod).Diagnosis)
		return truncate(summary, m.layout().width) + "\n" +
			styleHelp("Tab: next panel · e: all evidence") + "\n" + panel(m.podDashboard.panel)
	}
	middle := lipgloss.JoinHorizontal(lipgloss.Top, panel(podContext), " ",
		lipgloss.JoinVertical(lipgloss.Left, panel(podComposition), panel(podHistory)), " ", panel(podChecks))
	bottom := lipgloss.JoinHorizontal(lipgloss.Top, panel(podContainers), " ", panel(podSignals))
	return lipgloss.JoinVertical(lipgloss.Left, m.podSummary(pod), "", middle, "", bottom)
}

func (m appModel) podSummary(pod api.PodSnapshot) string {
	finding := explain.AnalyzePod(pod)
	values := []string{
		memmodel.FormatBytes(pod.Memory.TotalBytes) + "\nObserved cgroup charge",
		podLimit(pod) + "\n" + limitLabel(pod.Memory, 12, false),
		podHeadroom(pod) + "\nUntil configured limit",
		incidentLabel(pod.Memory) + "\n" + m.podEvidenceLabel(pod),
		string(finding.Diagnosis) + " · " + string(finding.Severity) + "\nConfidence: " + string(finding.Confidence),
	}
	titles := []string{"POD CHARGE", "MEMORY LIMIT", "HEADROOM", "CURRENT SIGNALS", "FINDING"}
	width := m.layout().width
	base := (width - 4) / 5
	var panels []string
	for i := range titles {
		w := base
		if i < (width-4)%5 {
			w++
		}
		panels = append(panels, m.dashboardPanel(titles[i], truncateLines(strings.Split(values[i], "\n"), w-2), w, 5, false))
		if i < 4 {
			panels = append(panels, " ")
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, panels...)
}

func podLimit(pod api.PodSnapshot) string {
	if !pod.Memory.MaxKnown {
		return "unknown"
	}
	if pod.Memory.MaxUnlimited || pod.Memory.MaxBytes == 0 {
		return "unlimited"
	}
	return memmodel.FormatCompactBytes(pod.Memory.MaxBytes)
}

func podHeadroom(pod api.PodSnapshot) string {
	memory := pod.Memory
	if !memory.MaxKnown || memory.MaxUnlimited || memory.MaxBytes == 0 {
		return "not available"
	}
	if memory.TotalBytes > memory.MaxBytes {
		return "Exceeded by " + memmodel.FormatCompactBytes(memory.TotalBytes-memory.MaxBytes)
	}
	return memmodel.FormatBytes(memory.MaxBytes - memory.TotalBytes)
}

func (m appModel) podDashboardHeader(width int) string {
	return headerStyle.Render(truncate("KubeMemLens / Pods / "+m.detail.namespace+" / "+m.detail.podName, width))
}

func (m appModel) podDashboardFooter(width int) string {
	return styleHelp(truncate("Esc main · Tab panel · j/k scroll/select · Enter open · e raw · R checks · space pause · r refresh · ? help", width))
}

func (m appModel) renderPodDashboardHelp(width int) string {
	lines := []string{
		"Pod dashboard", "",
		"Esc / h: return to the previous dashboard, preserving selection and filter.",
		"Tab / Shift+Tab: focus the next / previous panel.",
		"j/k or arrows: scroll the panel; select a check or container in its panel.",
		"PgUp/PgDown: scroll a page. g/G: first / last.",
		"Enter: open the selected check or container. Esc closes that view first.",
		"e: read all evidence. R: read-only recommendations. x: compare Pods.",
		"Space: pause refresh. r: refresh once. q: quit. ? / Esc: close help.",
	}
	lines = wrapText(lines, width)
	return truncateLines(lines[:minInt(len(lines), m.bodyRows())], width)
}

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true)
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (m appModel) View() string {
	width := m.width
	if width <= 0 {
		width = 100
	}

	var b strings.Builder
	b.WriteString(m.renderHeader(width))
	b.WriteString("\n\n")
	if m.help {
		b.WriteString(m.renderHelp(width))
	} else if m.statusErr != nil && len(m.data.Namespaces) == 0 {
		b.WriteString(m.renderConnectionError(width))
	} else if len(m.data.Namespaces) == 0 && m.loading {
		b.WriteString("Loading collector snapshots...")
	} else if len(m.data.Namespaces) == 0 && m.statusErr == nil && !m.loading {
		b.WriteString(m.renderEmpty(width))
	} else {
		switch m.view {
		case viewNamespaces:
			b.WriteString(m.renderNamespaces(width))
		case viewPods:
			b.WriteString(m.renderPods(width))
		case viewContainers:
			b.WriteString(m.renderContainers(width))
		case viewDetail:
			b.WriteString(m.renderDetail(width))
		}
	}
	b.WriteString("\n")
	b.WriteString(m.renderFooter(width))
	return b.String()
}

func (m appModel) renderHeader(width int) string {
	parts := []string{
		"KubeMemLens",
		"view: " + m.view.String(),
		"connection: " + m.connectionDescription,
		"sort: " + m.sort.String(),
	}
	if ns, all := m.activeNamespace(); !all && ns != "" {
		parts = append(parts, "namespace: "+ns)
	}
	if m.query != "" || m.searching {
		parts = append(parts, "filter: "+m.query)
	}
	if m.lastRefresh.IsZero() {
		parts = append(parts, "refreshed: never")
	} else {
		parts = append(parts, "refreshed: "+FormatAge(m.lastRefresh)+" ago")
	}
	if m.loading {
		parts = append(parts, "refreshing")
	}
	if m.statusErr != nil {
		parts = append(parts, errorStyle.Render("status: connection error"))
	}
	return headerStyle.Render(truncate(strings.Join(parts, " | "), width))
}

func (m appModel) renderFooter(width int) string {
	footer := "q quit · r refresh · / search · tab switch · enter drill · e explain · ? help"
	if m.searching {
		footer = "search: " + m.query + " · enter keep · esc clear · backspace delete"
	}
	return helpStyle.Render(truncate(footer, width))
}

func (m appModel) renderHelp(width int) string {
	lines := []string{
		"Keybindings",
		"",
		"q / Ctrl+C   quit",
		"r            refresh now",
		"/            search current table",
		"Esc          clear search or leave detail view",
		"Tab          cycle namespace, pod, and container views",
		"n / p / c    jump to namespace, pod, or container view",
		"Enter        drill into namespace, pod, or container's pod",
		"e            explain selected pod",
		"h / Backspace go back",
		"k/j or arrows move selection",
		"PgUp/PgDown  move faster",
		"s            cycle sort: total, rss, cache, shmem, name",
	}
	return truncateLines(lines, width)
}

func (m appModel) renderEmpty(width int) string {
	lines := []string{
		"No collector snapshots yet.",
		"",
		"Check that the agent is running and posting snapshots:",
		"  kubectl logs -n kube-memlens ds/kube-memlens-agent",
		"  go run ./cmd/kubectl-memlens status",
	}
	return truncateLines(lines, width)
}

func (m appModel) renderConnectionError(width int) string {
	lines := strings.Split(statusError(m.opts.ConnectionOptions, m.connectionDescription, m.statusErr), "\n")
	return errorStyle.Render(truncateLines(lines, width))
}

func (m appModel) renderNamespaces(width int) string {
	items := m.visibleNamespaces()
	if len(items) == 0 {
		return "No namespaces match the current filter."
	}
	widths := namespaceWidths(width)
	lines := []string{tableRow([]string{"NAMESPACE", "PODS", "TOTAL", "RSS", "CACHE", "SHMEM", "SLAB", "DIAGNOSIS", "AGE"}, widths, nil)}
	for i, item := range items {
		prefix := " "
		if i == m.selected {
			prefix = "›"
		}
		lines = append(lines, prefix+tableRow([]string{
			item.Namespace,
			fmt.Sprintf("%d", item.PodCount),
			memmodel.FormatCompactBytes(item.Memory.TotalBytes),
			memmodel.FormatCompactBytes(item.Memory.RSSBytes()),
			memmodel.FormatCompactBytes(item.Memory.CacheBytes()),
			memmodel.FormatCompactBytes(item.Memory.ShmemBytes),
			memmodel.FormatCompactBytes(item.Memory.SlabBytes),
			string(explain.Analyze(item.Memory).Diagnosis),
			FormatAge(item.CapturedAt),
		}, widths, numericIndexes(1, 2, 3, 4, 5, 6)))
	}
	return truncateLines(limitLines(lines, m.bodyRows()), width)
}

func (m appModel) renderPods(width int) string {
	items := m.visiblePods()
	if len(items) == 0 {
		return "No pods match the current filter."
	}
	widths := podWidths(width)
	lines := []string{tableRow([]string{"NAMESPACE", "POD", "NODE", "TOTAL", "RSS", "CACHE", "SHMEM", "SLAB", "DIAGNOSIS", "AGE"}, widths, nil)}
	for i, item := range items {
		prefix := " "
		if i == m.selected {
			prefix = "›"
		}
		lines = append(lines, prefix+tableRow([]string{
			item.Namespace,
			item.PodName,
			item.NodeName,
			memmodel.FormatCompactBytes(item.Memory.TotalBytes),
			memmodel.FormatCompactBytes(item.Memory.RSSBytes()),
			memmodel.FormatCompactBytes(item.Memory.CacheBytes()),
			memmodel.FormatCompactBytes(item.Memory.ShmemBytes),
			memmodel.FormatCompactBytes(item.Memory.SlabBytes),
			string(explain.Analyze(item.Memory).Diagnosis),
			FormatAge(item.CapturedAt),
		}, widths, numericIndexes(3, 4, 5, 6, 7)))
	}
	return truncateLines(limitLines(lines, m.bodyRows()), width)
}

func (m appModel) renderContainers(width int) string {
	items := m.visibleContainers()
	if len(items) == 0 {
		return "No containers match the current filter."
	}
	widths := containerWidths(width)
	lines := []string{tableRow([]string{"NAMESPACE", "POD", "CONTAINER", "NODE", "TOTAL", "RSS", "CACHE", "SHMEM", "SLAB", "DIAGNOSIS", "AGE"}, widths, nil)}
	for i, item := range items {
		prefix := " "
		if i == m.selected {
			prefix = "›"
		}
		lines = append(lines, prefix+tableRow([]string{
			item.Namespace,
			item.PodName,
			item.ContainerName,
			item.NodeName,
			memmodel.FormatCompactBytes(item.Memory.TotalBytes),
			memmodel.FormatCompactBytes(item.Memory.RSSBytes()),
			memmodel.FormatCompactBytes(item.Memory.CacheBytes()),
			memmodel.FormatCompactBytes(item.Memory.ShmemBytes),
			memmodel.FormatCompactBytes(item.Memory.SlabBytes),
			string(explain.Analyze(item.Memory).Diagnosis),
			FormatAge(item.CapturedAt),
		}, widths, numericIndexes(4, 5, 6, 7, 8)))
	}
	return truncateLines(limitLines(lines, m.bodyRows()), width)
}

func (m appModel) renderDetail(width int) string {
	pod, ok := m.selectedPod()
	if !ok {
		return "Selected pod is no longer present in collector snapshots."
	}
	result := explain.Analyze(pod.Memory)
	lines := []string{
		"Pod: " + pod.PodName,
		"Namespace: " + pod.Namespace,
		"Node: " + pod.NodeName,
		"Captured: " + pod.CapturedAt.Format("2006-01-02 15:04:05 MST") + " (" + FormatAge(pod.CapturedAt) + " ago)",
		"",
		"Memory breakdown:",
		"Total charged memory: " + memmodel.FormatBytes(pod.Memory.TotalBytes),
		"RSS / anon:           " + memmodel.FormatBytes(pod.Memory.RSSBytes()),
		"File cache:           " + memmodel.FormatBytes(pod.Memory.CacheBytes()),
		"Active file:          " + memmodel.FormatBytes(pod.Memory.ActiveFileBytes),
		"Inactive file:        " + memmodel.FormatBytes(pod.Memory.InactiveFileBytes),
		"Shmem / tmpfs:        " + memmodel.FormatBytes(pod.Memory.ShmemBytes),
		"Slab / kernel:        " + memmodel.FormatBytes(pod.Memory.SlabBytes),
		"Dirty/writeback:      " + memmodel.FormatDirtyWriteback(pod.Memory),
		fmt.Sprintf("OOM events:           %d", pod.Memory.OOMEvents),
		fmt.Sprintf("OOM kill events:      %d", pod.Memory.OOMKillEvents),
		fmt.Sprintf("High events:          %d", pod.Memory.HighEvents),
		fmt.Sprintf("Max events:           %d", pod.Memory.MaxEvents),
		"",
		"Diagnosis:",
		string(result.Diagnosis),
		"",
		"Likely explanation:",
		result.LikelyExplanation,
		"",
		"Signals:",
	}
	for _, signal := range result.Signals {
		lines = append(lines, "- "+signal)
	}
	lines = append(lines, "", "Suggested checks:")
	for _, check := range result.SuggestedChecks {
		lines = append(lines, "- "+check)
	}
	lines = append(lines, "", "Containers:")
	lines = append(lines, renderContainerSummary(pod.Containers)...)
	return truncateLines(limitLines(lines, m.bodyRows()), width)
}

func renderContainerSummary(containers []api.ContainerSnapshot) []string {
	widths := []int{18, 8, 8, 8, 7, 8, 18}
	lines := []string{tableRow([]string{"CONTAINER", "TOTAL", "RSS", "CACHE", "SHMEM", "SLAB", "DIAGNOSIS"}, widths, nil)}
	for _, container := range containers {
		lines = append(lines, tableRow([]string{
			container.ContainerName,
			memmodel.FormatCompactBytes(container.Memory.TotalBytes),
			memmodel.FormatCompactBytes(container.Memory.RSSBytes()),
			memmodel.FormatCompactBytes(container.Memory.CacheBytes()),
			memmodel.FormatCompactBytes(container.Memory.ShmemBytes),
			memmodel.FormatCompactBytes(container.Memory.SlabBytes),
			string(explain.Analyze(container.Memory).Diagnosis),
		}, widths, numericIndexes(1, 2, 3, 4, 5)))
	}
	return lines
}

func tableRow(cells []string, widths []int, right map[int]struct{}) string {
	parts := make([]string, 0, len(cells))
	for i, cell := range cells {
		_, alignRight := right[i]
		parts = append(parts, pad(cell, widths[i], alignRight))
	}
	return strings.Join(parts, "  ")
}

func numericIndexes(indexes ...int) map[int]struct{} {
	out := map[int]struct{}{}
	for _, index := range indexes {
		out[index] = struct{}{}
	}
	return out
}

func truncateLines(lines []string, width int) string {
	for i, line := range lines {
		lines[i] = truncate(line, width)
	}
	return strings.Join(lines, "\n")
}

func limitLines(lines []string, max int) []string {
	if max <= 0 || len(lines) <= max {
		return lines
	}
	return lines[:max]
}

func (m appModel) bodyRows() int {
	if m.height <= 0 {
		return 25
	}
	rows := m.height - 5
	if rows < 5 {
		return 5
	}
	return rows
}

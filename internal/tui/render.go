package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/danushkastanley/kube-memlens/internal/explain"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true)
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (m appModel) View() string {
	plan := m.layout()
	width := plan.contentWidth()

	var b strings.Builder
	b.WriteString(m.renderHeader(width))
	b.WriteString("\n\n")
	if m.action.mode != actionClosed {
		b.WriteString(m.renderAction(width))
	} else if m.help {
		b.WriteString(m.renderHelp(width))
	} else if m.statusErr != nil && len(m.data.Namespaces) == 0 {
		b.WriteString(m.renderConnectionError(width))
	} else if len(m.data.Namespaces) == 0 && m.loading {
		b.WriteString("Loading collector snapshots...")
	} else if len(m.data.Namespaces) == 0 && m.statusErr == nil && !m.loading {
		b.WriteString(m.renderEmpty(width))
	} else {
		b.WriteString(m.renderContent(plan))
	}
	b.WriteString("\n")
	b.WriteString(m.renderFooter(width))
	return b.String()
}

func (m appModel) renderContent(plan layoutPlan) string {
	if m.view == viewDetail {
		return m.renderDetail(plan.width)
	}
	if m.view == viewPods && plan.mode == layoutWide {
		return m.renderWideDashboard(plan)
	}
	renderTable := func(width int) string {
		switch m.view {
		case viewNodes:
			return m.renderNodes(width)
		case viewNamespaces:
			return m.renderNamespaces(width)
		case viewWorkloads:
			return m.renderWorkloads(width)
		case viewPods:
			return m.renderPods(width)
		case viewContainers:
			return m.renderContainers(width)
		default:
			return ""
		}
	}
	if !plan.splitDetail {
		return renderTable(plan.width)
	}
	left := renderTable(plan.tableWidth)
	rightLines := m.inlineDetailLines(plan.detailWidth)
	viewport := m.viewports[viewDetail]
	viewport.resize(plan.bodyRows)
	viewport.reconcile(len(rightLines))
	right := truncateLines(viewportWindow(viewport, rightLines), plan.detailWidth)
	leftPane := lipgloss.NewStyle().Width(plan.tableWidth).Render(left)
	rightPane := lipgloss.NewStyle().Width(plan.detailWidth).Render(right)
	divider := helpStyle.Render("│")
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)
}

func (m appModel) renderHeader(width int) string {
	parts := []string{
		"KubeMemLens",
		"view: " + m.view.String(),
		"sort: " + m.sort.String(),
	}
	if ns, all := m.activeNamespace(); !all && ns != "" {
		parts = append(parts, "namespace: "+ns)
	}
	if m.currentWorkloadName != "" {
		parts = append(parts, "workload: "+m.currentWorkloadKind+"/"+m.currentWorkloadName)
	}
	if m.currentNode != "" {
		parts = append(parts, "node: "+m.currentNode)
	}
	if m.loading || m.paused {
		states := make([]string, 0, 2)
		if m.paused {
			states = append(states, "paused")
		}
		if m.loading {
			states = append(states, "refreshing")
		}
		parts = append(parts, "state: "+strings.Join(states, "/"))
	}
	if m.query != "" || m.searching {
		parts = append(parts, "filter: "+m.query)
	}
	if m.lastRefresh.IsZero() {
		parts = append(parts, "refreshed: never")
	} else {
		parts = append(parts, "refreshed: "+FormatAge(m.lastRefresh)+" ago")
	}
	if m.statusErr != nil {
		parts = append(parts, errorStyle.Render("status: connection error"))
	}
	if m.layout().splitDetail {
		focus := "table"
		if m.focus == focusDetail {
			focus = "detail"
		}
		parts = append(parts, "focus: "+focus)
	}
	return headerStyle.Render(truncate(strings.Join(parts, " | "), width))
}

func (m appModel) renderFooter(width int) string {
	footer := "q quit · space pause · r refresh · / filter · N/n/w/p/c views · enter drill · e explain · ? help"
	if m.searching {
		footer = "search: " + m.query + " · enter keep · esc clear · backspace delete"
	} else if m.view == viewDetail {
		viewport := m.viewports[viewDetail]
		viewport.reconcile(len(m.detailLines(width)))
		start, end := viewport.visibleRange()
		footer = fmt.Sprintf("detail %d–%d/%d · j/k scroll · PgUp/PgDown page · g/G ends · h back · q quit", start+1, end, viewport.count)
	} else if m.layout().splitDetail {
		footer = "tab focus · j/k move or scroll · PgUp/PgDown page · enter drill · N/n/w/p/c views · ? help"
	}
	if m.action.mode != actionClosed {
		footer = "Esc close · action keys shown above"
	}
	return helpStyle.Render(truncate(footer, width))
}

func (m appModel) renderHelp(width int) string {
	lines := []string{
		"Keybindings",
		"",
		"q / Ctrl+C   quit",
		"r            refresh now",
		"Space        pause/resume automatic refresh",
		"/            filter current table",
		"Esc          clear filter or leave detail view",
		"Tab          switch table/detail focus in wide layouts",
		"N / n / w / p / c jump to node, namespace, workload, pod, or container view",
		"Enter        drill into namespace, pod, or container's pod",
		"e            explain selected pod",
		"h / Backspace go back",
		"k/j or arrows move selection",
		"PgUp/PgDown  move faster",
		"g / G        jump to first or last row",
		"s            cycle sort: total, rss, cache, shmem, name",
		"a            incident action menu",
		"R / x / C    recommendations / compare / capture",
		"y            copy safe follow-up command",
	}
	return truncateLines(lines, width)
}

func (m appModel) renderWorkloads(width int) string {
	items := m.visibleWorkloads()
	if len(items) == 0 {
		return "No workloads match the current filter."
	}
	widths := workloadWidths(width)
	lines := []string{tableRow([]string{"NAMESPACE", "KIND", "WORKLOAD", "PODS", "TOTAL", "RSS", "CACHE", "SHMEM", "OTHER", "LARGEST", "MAX POD", "DIAGNOSIS"}, widths, nil)}
	viewport := m.viewports[viewWorkloads]
	viewport.reconcile(len(items))
	start, end := viewport.visibleRange()
	for i := start; i < end; i++ {
		item := items[i]
		prefix := " "
		if i == viewport.selected {
			prefix = "›"
		}
		lines = append(lines, prefix+tableRow([]string{
			item.Namespace, item.Kind, item.Name, fmt.Sprintf("%d", item.PodCount),
			memmodel.FormatCompactBytes(item.Memory.TotalBytes), memmodel.FormatCompactBytes(item.Memory.RSSBytes()),
			memmodel.FormatCompactBytes(item.Memory.CacheBytes()), memmodel.FormatCompactBytes(item.Memory.ShmemBytes),
			memmodel.FormatCompactBytes(item.Memory.ResidualBytes()), item.LargestPodName,
			memmodel.FormatCompactBytes(item.LargestPodBytes), string(explain.Analyze(item.Memory).Diagnosis),
		}, widths, numericIndexes(3, 4, 5, 6, 7, 8, 10)))
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
	lines := []string{tableRow([]string{"NAMESPACE", "PODS", "TOTAL", "RSS", "CACHE", "SHMEM", "OTHER", "DIAGNOSIS", "AGE"}, widths, nil)}
	viewport := m.viewports[viewNamespaces]
	viewport.reconcile(len(items))
	start, end := viewport.visibleRange()
	for i := start; i < end; i++ {
		item := items[i]
		prefix := " "
		if i == viewport.selected {
			prefix = "›"
		}
		lines = append(lines, prefix+tableRow([]string{
			item.Namespace,
			fmt.Sprintf("%d", item.PodCount),
			memmodel.FormatCompactBytes(item.Memory.TotalBytes),
			memmodel.FormatCompactBytes(item.Memory.RSSBytes()),
			memmodel.FormatCompactBytes(item.Memory.CacheBytes()),
			memmodel.FormatCompactBytes(item.Memory.ShmemBytes),
			memmodel.FormatCompactBytes(item.Memory.ResidualBytes()),
			string(explain.Analyze(item.Memory).Diagnosis),
			FormatAge(item.CapturedAt),
		}, widths, numericIndexes(1, 2, 3, 4, 5, 6)))
	}
	return truncateLines(lines, width)
}

func (m appModel) renderPods(width int) string {
	items := m.visiblePods()
	if len(items) == 0 {
		if m.query != "" {
			return fmt.Sprintf("No Pods match filter %q. Press Esc to clear it.", m.query)
		}
		return "No pods match the current filter."
	}
	compact := width < 115
	var widths []int
	var lines []string
	if compact {
		widths = withDynamic(width, []int{8, 12, 12, 11, 5}, 16)
		lines = []string{tableRow([]string{"POD", "TOTAL", "LIMIT", "A/F/S/O", "RISK", "AGE"}, widths, nil)}
	} else {
		widths = withTwoDynamic(width, []int{12, 8, 14, 16, 14, 14, 3, 6}, 12, 18)
		lines = []string{tableRow([]string{"NAMESPACE", "POD", "NODE", "TOTAL", "LIMIT", "A/F/S/O", "SIGNAL", "DIAGNOSIS", "TR", "AGE"}, widths, nil)}
	}
	viewport := m.viewports[viewPods]
	viewport.reconcile(len(items))
	start, end := viewport.visibleRange()
	for i := start; i < end; i++ {
		item := items[i]
		presentation := presentMemory(item.Memory, 12, false)
		trend := trendLabel(m.podTrends[podKey(item.Namespace, item.PodName)])
		prefix := " "
		if i == viewport.selected {
			prefix = "›"
		}
		if compact {
			identity := item.PodName
			if m.opts.AllNamespaces || m.currentNamespace == "" {
				identity = item.Namespace + "/" + item.PodName
			}
			lines = append(lines, prefix+tableRow([]string{
				identity,
				memmodel.FormatCompactBytes(item.Memory.TotalBytes),
				presentation.limit,
				presentation.composition,
				presentation.severity + " " + trend,
				formatPodAge(item),
			}, widths, numericIndexes(1)))
			continue
		}
		lines = append(lines, prefix+tableRow([]string{
			item.Namespace,
			item.PodName,
			item.NodeName,
			memmodel.FormatCompactBytes(item.Memory.TotalBytes),
			presentation.limit,
			presentation.composition,
			presentation.signal,
			presentation.diagnosis,
			trend,
			formatPodAge(item),
		}, widths, numericIndexes(3)))
	}
	return truncateLines(lines, width)
}

func (m appModel) renderContainers(width int) string {
	items := m.visibleContainers()
	if len(items) == 0 {
		return "No containers match the current filter."
	}
	widths := containerWidths(width)
	lines := []string{tableRow([]string{"NAMESPACE", "POD", "CONTAINER", "NODE", "TOTAL", "RSS", "CACHE", "SHMEM", "OTHER", "DIAGNOSIS", "AGE"}, widths, nil)}
	viewport := m.viewports[viewContainers]
	viewport.reconcile(len(items))
	start, end := viewport.visibleRange()
	for i := start; i < end; i++ {
		item := items[i]
		prefix := " "
		if i == viewport.selected {
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
			memmodel.FormatCompactBytes(item.Memory.ResidualBytes()),
			string(explain.Analyze(item.Memory).Diagnosis),
			FormatAge(item.CapturedAt),
		}, widths, numericIndexes(4, 5, 6, 7, 8)))
	}
	return truncateLines(lines, width)
}

func (m appModel) renderDetail(width int) string {
	lines := m.detailLines(width)
	viewport := m.viewports[viewDetail]
	viewport.reconcile(len(lines))
	return truncateLines(viewportWindow(viewport, lines), width)
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

func (m appModel) bodyRows() int {
	return m.layout().bodyRows
}

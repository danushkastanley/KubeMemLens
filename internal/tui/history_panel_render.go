package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/historyview"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func (m appModel) historyPanelLines(width int) []string {
	p := m.historyPanel
	name := p.request.Name
	if p.request.Namespace != "" {
		name = p.request.Namespace + "/" + name
	}
	label := "history"
	if p.mode == markedHistory {
		label = "history with changes"
	}
	lines := []string{fmt.Sprintf("%s %s (%s): %s", p.request.Scope, label, p.source, name)}
	if p.markers != nil {
		lines = append(lines, fmt.Sprintf("Changes: %s event history · %d markers", p.markers.Events, len(p.markers.Markers)))
	}
	if p.loading {
		lines = append(lines, "Loading "+string(p.source)+" history...")
	}
	if p.err != nil {
		lines = append(lines, "History unavailable: "+p.err.Error(), "Press l to request local history explicitly, or Esc for live evidence.")
	}
	if p.report != nil {
		lines = append(lines, historyview.Lines(*p.report, time.Now().UTC(), width)...)
	}
	if p.markers != nil {
		lines = append(lines, historyview.MarkerLines(*p.markers)...)
	}
	return wrapText(lines, width)
}

func (m appModel) renderHistoryPanel(width int) string {
	lines := m.historyPanelLines(width)
	v := m.historyPanel.viewport
	v.resize(max(1, m.bodyRows()-2))
	v.reconcile(len(lines))
	start, end := v.visibleRange()
	controls := "Memory trends: l local | p Prometheus | r refresh | Esc close"
	if m.historyPanel.request.Scope != memoryhistory.Node {
		controls = "Memory trends: l local | p Prometheus | m markers | r refresh | Esc close"
	}
	header := truncate(controls, width)
	footer := truncate(fmt.Sprintf("Lines %d-%d/%d | j/k scroll | PgUp/PgDown page", start+1, end, len(lines)), width)
	return header + "\n" + strings.Join(lines[start:end], "\n") + "\n" + footer
}

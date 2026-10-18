package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/replicaview"
)

func (m appModel) replicaPanelLines(width int) []string {
	p := m.replicaPanel
	lines := []string{fmt.Sprintf("Replica comparison: %s/%s", p.request.Namespace, p.request.Name)}
	if p.loading {
		lines = append(lines, "Loading authorised replica evidence...")
	}
	if p.err != nil {
		lines = append(lines, "Replica comparison unavailable: "+p.err.Error(), "Press r to retry or Esc to close.")
	}
	if p.report != nil {
		lines = append(lines, replicaview.Lines(*p.report, time.Now().UTC())...)
	}
	return wrapText(lines, width)
}

func (m appModel) renderReplicaPanel(width int) string {
	lines := m.replicaPanelLines(width)
	v := m.replicaPanel.viewport
	v.resize(max(1, m.bodyRows()-2))
	v.reconcile(len(lines))
	start, end := v.visibleRange()
	return truncate("Replica comparisons: r refresh | Esc close", width) + "\n" + strings.Join(lines[start:end], "\n") + "\n" + truncate(fmt.Sprintf("Lines %d-%d/%d | j/k scroll | PgUp/PgDown page", start+1, end, len(lines)), width)
}

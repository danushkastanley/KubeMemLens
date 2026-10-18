package tui

import (
	"fmt"
	"strings"
)

func (m appModel) actionResultLines(width int) []string {
	title := m.action.result.title
	if title == "" {
		title = "Incident action"
	}
	lines := []string{title, ""}
	if m.action.inFlight {
		lines = append(lines, "Working…")
	} else if m.action.err != nil {
		lines = append(lines, "Error: "+m.action.err.Error())
	}
	lines = append(lines, m.action.result.lines...)
	if m.action.result.overwriteRequired {
		lines = append(lines, "", "Destination: "+m.action.result.outputPath, "Press f to confirm replacement, or Esc to cancel.")
	}
	return wrapText(lines, width)
}

func (m appModel) renderActionResult(width int) string {
	lines := m.actionResultLines(width)
	v := m.action.viewport
	v.resize(max(1, m.bodyRows()-2))
	v.reconcile(len(lines))
	start, end := v.visibleRange()
	return truncate("Incident result: Enter/Esc close | g/G first/last", width) + "\n" +
		strings.Join(lines[start:end], "\n") + "\n" +
		truncate(fmt.Sprintf("Lines %d-%d/%d | j/k scroll | PgUp/PgDown page", start+1, end, len(lines)), width)
}

func (m *appModel) scrollActionResult(key string) bool {
	v := &m.action.viewport
	v.resize(max(1, m.bodyRows()-2))
	v.reconcile(len(m.actionResultLines(m.layout().contentWidth())))
	switch key {
	case "j", "down":
		v.move(1)
	case "k", "up":
		v.move(-1)
	case "pgdown":
		v.move(v.capacity)
	case "pgup":
		v.move(-v.capacity)
	case "g":
		v.first()
	case "G":
		v.last()
	default:
		return false
	}
	return true
}

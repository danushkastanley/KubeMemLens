package tui

import (
	"fmt"
	"strings"
)

func (m appModel) renderAction(width int) string {
	lines := []string{}
	switch m.action.mode {
	case actionMenu:
		lines = []string{
			"Incident actions",
			"",
			"r  Preview read-only recommendations",
			"x  Mark/compare two live Pods",
			"c  Capture selected Pod to a redacted incident file",
			"y  Copy a safe follow-up command with OSC 52",
			"i  Manage a bounded incident session",
			"",
			"Session actions write explicitly created incident records.",
			"Esc closes this menu.",
		}
		if m.restricted() {
			lines[0] = "Restricted actions"
			lines[3] = "x  Mark/compare working-set observations"
			lines[4] = "c  Capture selected Pod (restricted schema 3)"
			lines[6] = "Sessions require the authenticated deep API."
			lines[8] = "Restricted evidence remains read-only."
		} else if ref, ok := m.currentActionRef(); ok && ref.kind == entityNode {
			lines[0] = "Node incident actions"
			lines[2] = "Node evidence is read-only; inspect signals in detail."
			lines[3] = "Compare Node captures with the CLI --node selector."
			lines[4] = "c  Capture selected Node (redacted schema 4)"
		}
	case actionCapturePath:
		lines = []string{
			"Redacted incident capture",
			"",
			"Enter an explicit destination path:",
			m.action.input + "█",
			"",
			"Enter writes mode 0600 · Esc cancels · existing files are never replaced silently",
		}
	case actionResultMode:
		return m.renderActionResult(width)
	default:
		return ""
	}
	maxRows := m.bodyRows()
	if len(lines) > maxRows {
		lines = lines[:maxRows]
	}
	for index, line := range lines {
		lines[index] = truncate(line, width)
	}
	return strings.Join(lines, "\n") + fmt.Sprintf("%s", "")
}

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
			"",
			"No action mutates Kubernetes resources.",
			"Esc closes this menu.",
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
		title := m.action.result.title
		if title == "" {
			title = "Incident action"
		}
		lines = append(lines, title, "")
		if m.action.inFlight {
			lines = append(lines, "Working…")
		} else if m.action.err != nil {
			lines = append(lines, "Error: "+m.action.err.Error())
		}
		lines = append(lines, m.action.result.lines...)
		if m.action.result.overwriteRequired {
			lines = append(lines, "", "Destination: "+m.action.result.outputPath, "Press f to confirm replacement, or Esc to cancel.")
		}
		if !m.action.inFlight && !m.action.result.overwriteRequired {
			lines = append(lines, "", "Enter or Esc closes this result.")
		}
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

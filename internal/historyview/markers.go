package historyview

import (
	"fmt"
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
)

func ContextLines(c changemarkers.Context, now time.Time, width int) []string {
	lines := Lines(c.History, now, width)
	return append(lines, MarkerLines(c.Changes)...)
}

func MarkerLines(report changemarkers.Report) []string {
	lines := []string{"", "Workload changes · event history: " + string(report.Events),
		"Retained events are best-effort reports, not a complete change log.",
		"Source clocks may differ; correlation does not establish causation."}
	if report.Truncated {
		lines = append(lines, "Marker or source budget reached; some changes are omitted.")
	}
	if len(report.Markers) == 0 {
		return append(lines, "No timed changes were retained for this selection and window.")
	}
	for _, m := range report.Markers {
		when := m.At.UTC().Format("02 Jan 15:04:05.000000") + " UTC"
		if !m.At.Equal(m.Until) {
			when += " to " + m.Until.UTC().Format("02 Jan 15:04:05.000000") + " UTC"
		}
		description := string(m.Kind)
		if m.ResizeState != "" {
			description += " · " + m.ResizeState
		}
		if m.Kind == changemarkers.Revision {
			description = "revision " + strconv.FormatInt(m.Revision, 10) + " observed; change time unknown"
		}
		subject := m.Subject.Kind + " " + m.Subject.Name
		if m.Container != "" {
			subject += "/" + m.Container
		}
		lines = append(lines, fmt.Sprintf("%s · %s · %s", when, subject, description), fmt.Sprintf("Source: %s · observations %d", m.Clock, m.Count))
		if m.Uncertain && m.Kind != changemarkers.Revision {
			lines = append(lines, "Timing/coverage is incomplete; interval does not imply continuous activity.")
		}
	}
	return lines
}

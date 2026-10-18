package tui

import (
	"fmt"
	"github.com/rivo/uniseg"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (m appModel) sessionPanelLines(width int) []string {
	p := m.sessionPanel
	lines := []string{"Incident session · namespace " + strconv.Quote(p.namespace)}
	if p.pod != "" {
		lines = append(lines, "Selected Pod: "+strconv.Quote(p.pod))
	}
	if p.id != "" {
		lines = append(lines, "Session: "+p.id)
	}
	if p.summary.ID != "" {
		lines = append(lines, fmt.Sprintf("%d entries · latest %s · expires %s", p.summary.Entries, p.summary.Latest.Kind, p.summary.ExpiresAt.Format(time.RFC3339)))
		if p.summary.Latest.GapReason != "" {
			lines = append(lines, "Evidence gap: "+strings.ReplaceAll(p.summary.Latest.GapReason, "-", " "))
		}
		if p.summary.Latest.ClockUncertain {
			lines = append(lines, "Clock uncertainty is recorded.")
		}
		if p.summary.LimitReached {
			lines = append(lines, "Session limit reached; existing evidence is retained.")
		}
	}
	if p.loading {
		lines = append(lines, "Request in progress. Esc interrupts; an action may already have completed.")
	}
	if p.err != nil {
		lines = append(lines, "Session request: "+p.err.Error())
	}
	if p.mode != sessionMenu {
		prompt := ""
		switch p.mode {
		case sessionAttach:
			prompt = "Enter an existing session ID:"
		case sessionAnnotation:
			prompt = "Private annotation (masked, one line, at most 512 UTF-8 bytes):"
		case sessionComparison:
			prompt = "Before and after evidence aliases/digests (full export permission required):"
		case sessionExport:
			prompt = "Sanitised export destination (mode 0600; existing files are not replaced):"
		case sessionPrivateExport:
			prompt = "Full export destination: includes private annotations, identities and capture bytes."
		case sessionDelete:
			prompt = "Type delete to remove the session and all retained evidence:"
		case sessionTracePath:
			prompt = "Local report destination (mode 0600; existing files are not replaced):"
		case sessionTraceConfirm:
			prompt = "Type attach to save this terminal trace report and send its private reference. The report stays local; timings, counters and digests are sent. Source authenticity remains unverified."
		}
		input := strconv.Quote(p.input)
		if p.mode == sessionAnnotation {
			input = strings.Repeat("•", utf8.RuneCountInString(p.input))
		}
		lines = append(lines, "", prompt, sessionInputTail(input+"█", width), "Enter submits · Esc discards input")
	} else {
		lines = append(lines, "", "n start · a attach · r refresh timeline · t private annotation", "c capture Pod · m local markers · M Prometheus markers", "T reference terminal trace · x compare aliases", "z close session · d delete · e sanitised export · E full authorised export", "Closing this panel leaves the server session until expiry or deletion.", "")
		lines = append(lines, p.lines...)
	}
	return wrapText(lines, width)
}

func (m appModel) renderSessionPanel(width int) string {
	lines := m.sessionPanelLines(width)
	v := m.sessionPanel.viewport
	v.resize(max(1, m.bodyRows()-2))
	v.reconcile(len(lines))
	if m.sessionPanel.mode != sessionMenu {
		v.last()
	}
	start, end := v.visibleRange()
	heading := "Incident session · Esc closes · j/k scroll · PgUp/PgDown page"
	if m.sessionPanel.mode != sessionMenu && m.sessionPanel.id != "" {
		heading = "Session " + m.sessionPanel.id
	}
	return truncate(heading, width) + "\n" + strings.Join(lines[start:end], "\n") + "\n" + truncate(fmt.Sprintf("Lines %d-%d/%d", start+1, end, len(lines)), width)
}

func sessionInputTail(value string, width int) string {
	if uniseg.StringWidth(value) <= width {
		return value
	}
	skip := uniseg.StringWidth(value) - max(1, width-1)
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		skip -= uniseg.StringWidth(graphemes.Str())
		if skip <= 0 {
			_, end := graphemes.Positions()
			return "…" + value[end:]
		}
	}
	return "…"
}

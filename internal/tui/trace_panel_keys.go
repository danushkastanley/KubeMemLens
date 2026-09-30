package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

func (m appModel) tracePanelKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := &m.tracePanel
	p.viewport.resize(max(1, m.bodyRows()-1))
	p.viewport.reconcile(len(wrapText(m.tracePanelLines(), m.layout().contentWidth())))
	key := msg.String()
	if key == "ctrl+c" || key == "q" && p.exportMode == "" {
		return m, tea.Quit
	}
	if p.exportMode != "" {
		return m.traceExportKey(msg)
	}
	switch key {
	case "esc", "T":
		p.open = false
	case "s":
		if p.session != nil {
			p.session.Cancel()
			p.snapshot = p.session.Snapshot()
		}
	case "n":
		if !p.loading && (p.session == nil || p.snapshot.State.Terminal()) {
			m.selectTraceTarget()
		}
	case "enter":
		if p.session == nil {
			return m, m.prepareTrace()
		}
		if p.snapshot.State == traceclient.StateReady {
			p.err = p.session.Start(m.ctx)
			p.snapshot = p.session.Snapshot()
			if p.err == nil {
				return m, m.traceUpdateCmd()
			}
		}
	case "x":
		if p.snapshot.State.Terminal() && !p.revoked {
			p.exportMode = "path"
			p.exportPath = "trace-report.json"
			p.err = nil
		}
	case "j", "down":
		p.viewport.move(1)
	case "k", "up":
		p.viewport.move(-1)
	case "pgdown":
		p.viewport.move(p.viewport.capacity)
	case "pgup":
		p.viewport.move(-p.viewport.capacity)
	default:
		if p.session != nil || p.loading {
			return m, nil
		}
		switch key {
		case "f":
			p.intent.Kind = trace.Files
		case "c":
			p.intent.Kind = trace.Cache
		case "o":
			p.intent.Kind = trace.OOM
		case "d":
			choices := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}
			for i, v := range choices {
				if p.intent.Bounds.Duration == v {
					p.intent.Bounds.Duration = choices[(i+1)%len(choices)]
					break
				}
			}
		case "l":
			choices := []uint64{100, 1000, 10000, 100000}
			for i, v := range choices {
				if p.intent.Bounds.Events == v {
					p.intent.Bounds.Events = choices[(i+1)%len(choices)]
					break
				}
			}
		}
	}
	return m, nil
}
func (m appModel) traceExportKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := &m.tracePanel
	if msg.String() == "esc" {
		p.exportMode = ""
		return m, nil
	}
	if p.exportMode == "saved" {
		return m, nil
	}
	if p.exportMode == "writing" {
		return m, nil
	}
	if p.exportMode == "confirm" {
		if msg.String() != "y" {
			return m, nil
		}
		document, err := tracereport.New(p.snapshot, buildinfo.Version, time.Now())
		if err != nil {
			p.err = err
			p.exportMode = ""
			return m, nil
		}
		path, generation := p.exportPath, p.generation
		p.exportMode = "writing"
		return m, func() tea.Msg {
			return traceExportMsg{generation: generation, err: incident.WriteTrace(nil, path, false, document)}
		}
	}
	switch msg.String() {
	case "enter":
		if strings.TrimSpace(p.exportPath) == "" || p.exportPath == "-" || strings.ContainsAny(p.exportPath, "\x00\r\n\x1b") {
			p.err = fmt.Errorf("enter a local file path")
			return m, nil
		}
		p.exportMode, p.err = "confirm", nil
	case "backspace":
		runes := []rune(p.exportPath)
		if len(runes) > 0 {
			p.exportPath = string(runes[:len(runes)-1])
		}
	default:
		if text := msg.Key().Text; text != "" && len(p.exportPath)+len(text) <= 4096 {
			p.exportPath += text
		}
	}
	return m, nil
}

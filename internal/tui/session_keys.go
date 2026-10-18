package tui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func (m appModel) sessionPanelKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := &m.sessionPanel
	key := message.String()
	if key == "ctrl+c" || (key == "q" && p.mode == sessionMenu) {
		m.clearSessionPanel()
		m.cancelHistoryRequest()
		m.cancelNodeRequest()
		m.cancelVolumeRequest()
		return m, tea.Quit
	}
	if key == "esc" {
		if p.loading {
			m.closeSessionPanel()
			m.setActionError(fmt.Errorf("the interrupted session request may have completed; no retry was sent"))
		} else if p.mode != sessionMenu {
			p.input = ""
			p.tracePath = ""
			p.err = nil
			p.mode = sessionMenu
		} else {
			m.closeSessionPanel()
		}
		return m, nil
	}
	if p.loading {
		return m, nil
	}
	if p.mode != sessionMenu {
		switch key {
		case "enter":
			command := m.submitSessionInput()
			return m, command
		case "backspace":
			runes := []rune(p.input)
			if len(runes) > 0 {
				p.input = string(runes[:len(runes)-1])
			}
			p.err = nil
		default:
			if message.Key().Text != "" {
				m.appendSessionInput(message.Key().Text)
			}
		}
		return m, nil
	}
	p.viewport.resize(max(1, m.bodyRows()-2))
	p.viewport.reconcile(len(m.sessionPanelLines(m.layout().contentWidth())))
	switch key {
	case "I":
		m.closeSessionPanel()
	case "n":
		if p.id != "" {
			p.err = fmt.Errorf("a session is attached; use another namespace or delete it before creating another")
			return m, nil
		}
		command := m.startSessionWork(sessionRequest{operation: sessionStart})
		return m, command
	case "a":
		p.viewport.reset()
		p.mode = sessionAttach
		p.input = ""
		p.err = nil
	case "r":
		command := m.startSessionWork(sessionRequest{operation: sessionRefresh})
		return m, command
	case "t":
		p.viewport.reset()
		p.mode = sessionAnnotation
		p.input = ""
		p.err = nil
	case "T":
		m.prepareSessionTraceReference()
	case "x":
		p.viewport.reset()
		p.mode = sessionComparison
		p.input = ""
		p.err = nil
	case "c":
		if p.pod == "" {
			p.err = fmt.Errorf("select a Pod before collecting evidence")
			return m, nil
		}
		command := m.startSessionWork(sessionRequest{operation: sessionCapturePod})
		return m, command
	case "m", "M":
		if p.pod == "" {
			p.err = fmt.Errorf("select a Pod before collecting markers")
			return m, nil
		}
		source := memoryhistory.Local
		if key == "M" {
			source = memoryhistory.Prometheus
		}
		command := m.startSessionWork(sessionRequest{operation: sessionMarkers, source: source})
		return m, command
	case "z":
		command := m.startSessionWork(sessionRequest{operation: sessionClose})
		return m, command
	case "d":
		p.viewport.reset()
		p.mode = sessionDelete
		p.input = ""
		p.err = nil
	case "e":
		p.viewport.reset()
		p.mode = sessionExport
		p.input = ""
		p.err = nil
	case "E":
		p.viewport.reset()
		p.mode = sessionPrivateExport
		p.input = ""
		p.err = nil
	case "j", "down":
		p.viewport.move(1)
	case "k", "up":
		p.viewport.move(-1)
	case "pgdown":
		p.viewport.move(p.viewport.capacity)
	case "pgup":
		p.viewport.move(-p.viewport.capacity)
	case "g":
		p.viewport.first()
	case "G":
		p.viewport.last()
	}
	return m, nil
}

func (m *appModel) appendSessionInput(text string) {
	p := &m.sessionPanel
	limit := 1024
	if p.mode == sessionAnnotation {
		limit = 512
	}
	if p.mode == sessionAttach {
		limit = 32
	}
	if p.mode == sessionComparison {
		limit = 256
	}
	if p.mode == sessionDelete || p.mode == sessionTraceConfirm {
		limit = 6
	}
	if !utf8.ValidString(text) || len(p.input)+len(text) > limit || strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) }) {
		p.err = fmt.Errorf("input contains unsupported text or exceeds its limit")
		return
	}
	p.input += text
	p.err = nil
}

func (m *appModel) submitSessionInput() tea.Cmd {
	p := &m.sessionPanel
	request := sessionRequest{input: p.input}
	switch p.mode {
	case sessionTracePath:
		if strings.TrimSpace(p.input) == "" || strings.TrimSpace(p.input) == "-" {
			p.err = fmt.Errorf("enter a named local report destination")
			return nil
		}
		p.tracePath, p.input, p.mode = p.input, "", sessionTraceConfirm
		return nil
	case sessionTraceConfirm:
		return m.confirmSessionTraceReference()
	case sessionAttach:
		if len(p.input) != 32 || strings.Trim(p.input, "0123456789abcdef") != "" {
			p.err = incidentsession.ErrInvalid
			return nil
		}
		p.id = p.input
		p.summary = incidentsession.Summary{}
		request.operation = sessionRefresh
	case sessionAnnotation:
		if incidentsession.ValidateAnnotation(p.input) != nil {
			p.err = incidentsession.ErrInvalid
			return nil
		}
		request.operation = sessionAnnotate
	case sessionComparison:
		if len(strings.Fields(p.input)) != 2 {
			p.err = fmt.Errorf("enter two evidence aliases or digests separated by a space")
			return nil
		}
		request.operation = sessionCompareEvidence
	case sessionExport:
		request.operation = sessionSavePublic
	case sessionPrivateExport:
		request.operation = sessionSavePrivate
	case sessionDelete:
		if p.input != "delete" {
			p.err = fmt.Errorf("type delete to remove this session and its retained evidence")
			return nil
		}
		request.operation = sessionRemove
	default:
		return nil
	}
	return m.startSessionWork(request)
}

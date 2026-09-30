package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
)

type sessionInput int

const (
	sessionMenu sessionInput = iota
	sessionAttach
	sessionAnnotation
	sessionComparison
	sessionExport
	sessionPrivateExport
	sessionDelete
	sessionTracePath
	sessionTraceConfirm
)

type sessionPanel struct {
	open, loading             bool
	namespace, pod, id, input string
	tracePath                 string
	traceGeneration           uint64
	mode                      sessionInput
	connection                client.IncidentSessions
	generation                uint64
	cancel                    context.CancelFunc
	summary                   incidentsession.Summary
	lines                     []string
	err                       error
	viewport                  viewport
}

func (sessionPanel) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[incident session panel]") }

type sessionPanelMsg struct {
	generation uint64
	operation  sessionOperation
	summary    incidentsession.Summary
	lines      []string
	err        error
}

func (m *appModel) openSessionPanel() tea.Cmd {
	namespace, pod := m.opts.Namespace, ""
	if selected, ok := m.currentActionPod(); ok {
		namespace, pod = selected.Namespace, selected.PodName
	} else if m.opts.AllNamespaces {
		namespace = m.currentNamespace
	}
	provider, ok := m.client.(client.IncidentSessionProvider)
	if m.restricted() || !ok || namespace == "" {
		m.setActionError(fmt.Errorf("incident sessions require an explicit namespace through the authenticated Kubernetes API"))
		return nil
	}
	if m.sessionPanel.namespace != namespace {
		m.clearSessionPanel()
	} else {
		m.closeSessionPanel()
	}
	connection, err := provider.OpenIncidentSessions(namespace)
	if err != nil {
		m.setActionError(err)
		return nil
	}
	p := &m.sessionPanel
	p.open, p.namespace, p.pod, p.connection = true, namespace, pod, connection
	m.action.mode = actionClosed
	if p.id != "" {
		return m.startSessionWork(sessionRequest{operation: sessionRefresh})
	}
	return nil
}

func (m *appModel) startSessionWork(request sessionRequest) tea.Cmd {
	p := &m.sessionPanel
	if p.loading || p.connection == nil {
		return nil
	}
	if request.operation != sessionStart && p.id == "" {
		p.err = fmt.Errorf("start or attach a session first")
		return nil
	}
	request.id, request.pod = p.id, p.pod
	request.namespace = p.namespace
	p.input = ""
	p.tracePath = ""
	p.mode = sessionMenu
	p.lines = nil
	p.err = nil
	p.viewport.reset()
	p.generation++
	generation, connection := p.generation, p.connection
	ctx, cancel := context.WithTimeout(m.ctx, 25*time.Second)
	p.cancel = cancel
	p.loading = true
	return func() tea.Msg {
		result := runSessionWork(ctx, connection, request)
		result.generation = generation
		return result
	}
}

func (m *appModel) receiveSessionPanel(message sessionPanelMsg) {
	p := &m.sessionPanel
	if !p.open || message.generation != p.generation {
		return
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false
	p.lines = nil
	p.err = message.err
	if message.err != nil {
		var failure *client.IncidentError
		if errors.As(message.err, &failure) && (failure.StatusCode == 401 || failure.StatusCode == 403 || failure.StatusCode == 404) {
			p.id = ""
			p.summary = incidentsession.Summary{}
		}
		return
	}
	if message.operation == sessionRemove {
		p.id = ""
		p.summary = incidentsession.Summary{}
	} else if message.summary.ID != "" {
		p.id = message.summary.ID
		p.summary = message.summary
	}
	p.lines = append([]string(nil), message.lines...)
}

func (m *appModel) closeSessionPanel() {
	p := &m.sessionPanel
	if p.cancel != nil {
		p.cancel()
	}
	if p.connection != nil {
		p.connection.Close()
	}
	p.generation++
	p.open, p.loading = false, false
	p.cancel = nil
	p.connection = nil
	p.input = ""
	p.lines = nil
	p.tracePath = ""
	p.traceGeneration = 0
	p.mode = sessionMenu
	p.err = nil
	p.viewport.reset()
}

func (m *appModel) clearSessionPanel() {
	m.closeSessionPanel()
	generation := m.sessionPanel.generation
	m.sessionPanel = sessionPanel{generation: generation}
}

func (m *appModel) expireSession(now time.Time) {
	p := &m.sessionPanel
	if p.summary.ExpiresAt.IsZero() || now.Before(p.summary.ExpiresAt) {
		return
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.generation++
	p.loading = false
	p.id = ""
	p.summary = incidentsession.Summary{}
	p.input = ""
	p.lines = nil
	p.tracePath = ""
	p.traceGeneration = 0
	p.mode = sessionMenu
	p.viewport.reset()
	p.err = fmt.Errorf("session expired; retained evidence is no longer available")
}

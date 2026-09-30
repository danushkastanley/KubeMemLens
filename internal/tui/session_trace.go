package tui

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

func (m *appModel) sessionTraceEligible() error {
	p, trace := &m.sessionPanel, &m.tracePanel
	if p.id == "" || p.connection == nil {
		return fmt.Errorf("start or attach an incident session first")
	}
	if trace.revoked || !trace.snapshot.State.Terminal() || trace.target.Namespace == "" || trace.target.Namespace != p.namespace {
		return fmt.Errorf("select a terminal trace from this incident namespace")
	}
	return nil
}

func (m *appModel) prepareSessionTraceReference() {
	p := &m.sessionPanel
	if err := m.sessionTraceEligible(); err != nil {
		p.err = err
		return
	}
	p.mode, p.input, p.tracePath = sessionTracePath, "", ""
	p.traceGeneration = m.tracePanel.generation
	p.err = nil
	p.viewport.reset()
}

func (m *appModel) confirmSessionTraceReference() tea.Cmd {
	p := &m.sessionPanel
	if p.input != "attach" {
		p.err = fmt.Errorf("type attach to save the report and send its private reference")
		return nil
	}
	if err := m.sessionTraceEligible(); err != nil {
		p.err = err
		return nil
	}
	if p.traceGeneration != m.tracePanel.generation {
		p.err = fmt.Errorf("the trace changed; select the report again")
		return nil
	}
	document, err := tracereport.New(m.tracePanel.snapshot, buildinfo.Version, time.Now())
	if err != nil {
		p.err = err
		return nil
	}
	return m.startSessionWork(sessionRequest{operation: sessionReferenceTrace, input: p.tracePath, traceDocument: document})
}

func saveSessionTraceReference(ctx context.Context, connection client.IncidentSessions, request sessionRequest) (incidentsession.Summary, error) {
	path := strings.TrimSpace(request.input)
	if path == "" || path == "-" {
		return incidentsession.Summary{}, fmt.Errorf("enter a named local report destination")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return incidentsession.Summary{}, fmt.Errorf("invalid local report destination")
	}
	data, err := request.traceDocument.Bytes()
	if err != nil {
		return incidentsession.Summary{}, err
	}
	if err := ctx.Err(); err != nil {
		return incidentsession.Summary{}, err
	}
	if err := incident.WriteTrace(io.Discard, absolute, false, request.traceDocument); err != nil {
		return incidentsession.Summary{}, err
	}
	result, err := connection.ReferenceIncidentTrace(ctx, request.id, data)
	if err != nil {
		return incidentsession.Summary{}, fmt.Errorf("report saved locally; incident attachment was not confirmed: %w", err)
	}
	return result, nil
}

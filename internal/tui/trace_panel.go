package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
)

type tracePanel struct {
	open, loading, available, revoked bool
	generation                        uint64
	client                            *traceclient.Client
	session                           *traceclient.Session
	target                            api.ContainerSnapshot
	intent                            traceclient.Intent
	snapshot                          traceclient.Snapshot
	err                               error
	viewport                          viewport
	exportPath                        string
	exportMode                        string
}
type traceDiscoveryMsg struct {
	client *traceclient.Client
	err    error
}
type tracePreparedMsg struct {
	generation uint64
	session    *traceclient.Session
	err        error
}
type traceUpdateMsg struct{ generation uint64 }
type traceExportMsg struct {
	generation uint64
	err        error
}

func (m appModel) discoverTraceCmd() tea.Cmd {
	provider, ok := m.client.(interface {
		NewTraceClient() (*traceclient.Client, error)
	})
	if !ok || m.restricted() {
		return nil
	}
	return func() tea.Msg {
		c, err := provider.NewTraceClient()
		if err != nil {
			return traceDiscoveryMsg{err: err}
		}
		if err := c.Discover(m.ctx); err != nil {
			c.Close()
			return traceDiscoveryMsg{err: err}
		}
		// Covers a result queued just as the terminal closes, too.
		context.AfterFunc(m.ctx, c.Close)
		return traceDiscoveryMsg{client: c}
	}
}
func (m *appModel) openTracePanel() {
	p := &m.tracePanel
	if !p.available || m.restricted() {
		m.setActionError(fmt.Errorf("optional trace actions are unavailable through this reader"))
		return
	}
	p.open = true
	if p.session != nil || p.loading {
		return
	}
	m.selectTraceTarget()
}
func (m *appModel) selectTraceTarget() {
	p := &m.tracePanel
	target, ok := m.currentActionContainer()
	if !ok {
		p.err = fmt.Errorf("select one container before opening a trace")
		return
	}
	generation, c, available := p.generation+1, p.client, p.available
	*p = tracePanel{open: true, generation: generation, client: c, available: available, target: target, intent: traceclient.DefaultIntent(trace.Files)}
}

func (m appModel) currentActionContainer() (api.ContainerSnapshot, bool) {
	ref, ok := m.currentActionRef()
	if !ok || ref.kind != entityContainer {
		return api.ContainerSnapshot{}, false
	}
	for _, row := range m.data.Containers {
		if row.Namespace == ref.namespace && row.PodName == ref.podName && row.ContainerName == ref.containerName {
			return row, true
		}
	}
	return api.ContainerSnapshot{}, false
}

func (m *appModel) revokeTraceEvidence() {
	p := &m.tracePanel
	p.revoked = true
	p.target = api.ContainerSnapshot{}
	p.snapshot = traceclient.Snapshot{}
	p.exportMode, p.exportPath = "", ""
	if p.session != nil {
		p.session.Cancel()
	}
}
func (m *appModel) prepareTrace() tea.Cmd {
	p := &m.tracePanel
	if p.loading || p.session != nil || p.target.ContainerID == "" || p.revoked {
		return nil
	}
	p.loading, p.err = true, nil
	c, target, intent, generation, ctx := p.client, p.target, p.intent, p.generation, m.ctx
	return func() tea.Msg {
		selection, err := c.SelectPinned(ctx, target.Namespace, target.PodName, target.ContainerName, traceclient.SelectionPin{PodUID: target.PodUID, ContainerID: target.ContainerID, NodeName: target.NodeName})
		if err != nil {
			return tracePreparedMsg{generation: generation, err: err}
		}
		session, err := traceclient.NewSession(c, selection, intent)
		if err == nil {
			err = session.Prepare(ctx)
		}
		return tracePreparedMsg{generation: generation, session: session, err: err}
	}
}
func (m *appModel) receiveTracePrepared(msg tracePreparedMsg) tea.Cmd {
	p := &m.tracePanel
	if msg.generation != p.generation || p.revoked {
		if msg.session != nil {
			msg.session.Cancel()
		}
		return nil
	}
	p.loading, p.session, p.err = false, msg.session, msg.err
	if p.session != nil {
		p.snapshot = p.session.Snapshot()
	}
	return nil
}
func (m appModel) traceUpdateCmd() tea.Cmd {
	session, generation, ctx := m.tracePanel.session, m.tracePanel.generation, m.ctx
	if session == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-session.Updates():
		case <-session.Done():
		case <-ctx.Done():
		}
		return traceUpdateMsg{generation: generation}
	}
}
func (m *appModel) receiveTraceUpdate(msg traceUpdateMsg) tea.Cmd {
	p := &m.tracePanel
	if msg.generation != p.generation || p.session == nil {
		return nil
	}
	p.snapshot = p.session.Snapshot()
	if p.revoked {
		p.snapshot.Selection = traceclient.Selection{}
		p.snapshot.Result = traceclient.Result{}
	}
	if p.snapshot.State.Terminal() {
		return nil
	}
	return m.traceUpdateCmd()
}

// Called after the program exits, before releasing the trace transport.
func (m appModel) shutdownTrace() error {
	p := m.tracePanel
	if p.session == nil {
		return nil
	}
	p.session.Cancel()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-p.session.Done():
		value := p.session.Snapshot()
		if value.Cleanup == traceclient.CleanupUnconfirmed {
			return fmt.Errorf("trace cleanup was not confirmed")
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("trace cleanup did not finish before the local shutdown deadline")
	}
}

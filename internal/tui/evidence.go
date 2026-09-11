package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

type discoveryMsg struct {
	generation uint64
	session    client.EvidenceSession
	err        error
}

func (m appModel) discoverCmd() tea.Cmd {
	return func() tea.Msg {
		session, err := client.NewEvidenceSession(m.ctx, m.opts.ConnectionOptions)
		if err == nil {
			err = session.Plan.Require(capability.Current)
		}
		return discoveryMsg{generation: m.fetchGeneration, session: session, err: err}
	}
}

func (m appModel) receiveDiscovery(msg discoveryMsg) (tea.Model, tea.Cmd) {
	if msg.generation != m.fetchGeneration {
		return m, nil
	}
	m.opts.EvidencePlan = &msg.session.Plan
	m.connectionDescription = msg.session.Description
	if msg.err != nil {
		m.loading, m.statusErr = false, msg.err
		return m, nil
	}
	m.client, m.statusErr = msg.session.Reader, nil
	return m, m.fetchCmd()
}

func (m appModel) evidenceLabel() string {
	if m.opts.EvidencePlan != nil {
		return m.opts.EvidencePlan.Label()
	}
	if m.client == nil {
		return "discovering sources"
	}
	return "deep / cgroup"
}

func (m appModel) loadingLabel() string {
	if m.client == nil {
		return "Discovering evidence sources..."
	}
	return "Loading collector snapshots..."
}

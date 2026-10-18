package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

type replicaPanel struct {
	open, loading  bool
	request        memoryhistory.Request
	expectedPodUID string
	generation     uint64
	cancel         context.CancelFunc
	report         *replicabaseline.Report
	err            error
	viewport       viewport
}
type replicaPanelMsg struct {
	generation uint64
	report     replicabaseline.Report
	err        error
}

func (m *appModel) openReplicaPanel() tea.Cmd {
	ref, ok := m.currentActionRef()
	if !ok || m.restricted() || (ref.kind != entityPod && ref.kind != entityWorkload) {
		m.setActionError(fmt.Errorf("select a Pod or workload through the authenticated replica API"))
		return nil
	}
	request := memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: ref.namespace, Name: ref.podName}
	if ref.kind == entityWorkload {
		request.Scope = memoryhistory.Workload
		request.Name = ref.name
		request.WorkloadKind = ref.workloadKind
	}
	if err := request.Validate(); err != nil {
		m.setActionError(err)
		return nil
	}
	m.closeReplicaPanel()
	m.replicaPanel.open = true
	m.replicaPanel.request = request
	if ref.kind == entityPod {
		if pod, found := m.currentActionPod(); found {
			m.replicaPanel.expectedPodUID = pod.PodUID
		}
	}
	return m.fetchReplicaPanel()
}

func (m *appModel) fetchReplicaPanel() tea.Cmd {
	p := &m.replicaPanel
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.generation++
	p.report = nil
	p.err = nil
	p.loading = false
	p.viewport.reset()
	reader, ok := m.client.(client.ReplicaReader)
	if !ok {
		p.err = fmt.Errorf("replica comparisons are unavailable through this reader")
		return nil
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	p.cancel = cancel
	p.loading = true
	request, generation := p.request, p.generation
	return func() tea.Msg {
		report, err := reader.ReplicaBaseline(ctx, request)
		return replicaPanelMsg{generation: generation, report: report, err: err}
	}
}

func (m *appModel) receiveReplicaPanel(msg replicaPanelMsg) {
	p := &m.replicaPanel
	if !p.open || msg.generation != p.generation {
		return
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false
	p.report = nil
	p.err = msg.err
	if msg.err != nil {
		return
	}
	if err := msg.report.Validate(); err != nil {
		p.err = err
		return
	}
	if p.expectedPodUID != "" {
		found := false
		for _, peer := range msg.report.Peers {
			found = found || (peer.Peer.Namespace == p.request.Namespace && peer.Peer.Name == p.request.Name && peer.Peer.UID == p.expectedPodUID)
		}
		if !found {
			p.err = fmt.Errorf("selected Pod changed; close comparisons and refresh the selection")
			return
		}
	}
	p.report = &msg.report
}

func (m *appModel) closeReplicaPanel() {
	if m.replicaPanel.cancel != nil {
		m.replicaPanel.cancel()
	}
	generation := m.replicaPanel.generation + 1
	m.replicaPanel = replicaPanel{generation: generation}
}

func (m appModel) replicaPanelKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := &m.replicaPanel
	p.viewport.resize(max(1, m.bodyRows()-2))
	p.viewport.reconcile(len(m.replicaPanelLines(m.layout().contentWidth())))
	switch msg.String() {
	case "esc", "B":
		m.closeReplicaPanel()
	case "q", "ctrl+c":
		m.closeReplicaPanel()
		m.cancelHistoryRequest()
		m.cancelNodeRequest()
		m.cancelVolumeRequest()
		return m, tea.Quit
	case "r":
		command := m.fetchReplicaPanel()
		return m, command
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

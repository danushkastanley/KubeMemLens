package tui

import (
	"context"
	"fmt"
	"net/url"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

type historyContextMode string

const (
	plainHistory  historyContextMode = ""
	markedHistory historyContextMode = "markers"
)

type historyPanel struct {
	mode                historyContextMode
	markers             *changemarkers.Report
	open                bool
	request             memoryhistory.Request
	expectedPodUID      string
	expectedContainerID string
	source              memoryhistory.Source
	generation          uint64
	loading             bool
	cancel              context.CancelFunc
	report              *memoryhistory.Report
	err                 error
	viewport            viewport
}

type historyPanelMsg struct {
	markers    *changemarkers.Report
	generation uint64
	report     memoryhistory.Report
	err        error
}

func (m *appModel) openHistoryPanel() tea.Cmd {
	ref, ok := m.currentActionRef()
	if !ok || m.restricted() {
		m.setActionError(fmt.Errorf("select a Pod, container, workload or Node through the authenticated history API"))
		return nil
	}
	request := memoryhistory.Request{Namespace: ref.namespace, Name: ref.podName}
	switch ref.kind {
	case entityPod:
		request.Scope = memoryhistory.Pod
	case entityContainer:
		request.Scope, request.Container = memoryhistory.Container, ref.containerName
	case entityWorkload:
		request.Scope, request.Name, request.WorkloadKind = memoryhistory.Workload, ref.name, ref.workloadKind
	case entityNode:
		request.Scope, request.Namespace, request.Name = memoryhistory.Node, "", ref.nodeName
	default:
		m.setActionError(fmt.Errorf("select a Pod, container, workload or Node for memory trends"))
		return nil
	}
	if err := request.Validate(); err != nil {
		m.setActionError(err)
		return nil
	}
	m.closeHistoryPanel()
	m.historyPanel.open = true
	m.historyPanel.request = request
	m.historyPanel.source = memoryhistory.Local
	if request.Scope == memoryhistory.Pod || request.Scope == memoryhistory.Container {
		if pod, found := m.currentActionPod(); found {
			m.historyPanel.expectedPodUID = pod.PodUID
		}
	}
	if request.Scope == memoryhistory.Container {
		for _, container := range m.data.Containers {
			if container.Namespace == request.Namespace && container.PodName == request.Name && container.ContainerName == request.Container && container.PodUID == m.historyPanel.expectedPodUID {
				m.historyPanel.expectedContainerID = container.ContainerID
				break
			}
		}
		if m.historyPanel.expectedContainerID == "" {
			m.historyPanel.err = fmt.Errorf("refresh the selected container before reading its history")
			return nil
		}
	}
	return m.fetchHistoryPanel()
}

func (m *appModel) fetchHistoryPanel() tea.Cmd {
	p := &m.historyPanel
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.generation++
	p.report = nil
	p.markers = nil
	p.err = nil
	p.viewport.reset()
	reader, ok := m.client.(client.MemoryHistoryReader)
	if !ok {
		p.loading = false
		p.err = fmt.Errorf("memory trends require the authenticated history API")
		return nil
	}
	query, err := memoryhistory.ParseQuery(url.Values{"source": {string(p.source)}}, p.request.Scope, time.Now().UTC())
	if err != nil {
		p.err = err
		p.loading = false
		return nil
	}
	timeout := 10 * time.Second
	if p.mode == markedHistory {
		timeout = 13 * time.Second
	}
	ctx, cancel := context.WithTimeout(m.ctx, timeout)
	p.cancel = cancel
	p.loading = true
	request, generation, mode := p.request, p.generation, p.mode
	markerReader, hasMarkers := m.client.(client.MemoryHistoryContextReader)
	return func() tea.Msg {
		if mode == markedHistory {
			if !hasMarkers {
				return historyPanelMsg{generation: generation, err: fmt.Errorf("workload change context is unavailable through this reader")}
			}
			result, err := markerReader.MemoryHistoryContext(ctx, request, query)
			return historyPanelMsg{generation: generation, report: result.History, markers: &result.Changes, err: err}
		}
		report, err := reader.MemoryHistory(ctx, request, query)
		return historyPanelMsg{generation: generation, report: report, err: err}
	}
}

func (m *appModel) receiveHistoryPanel(msg historyPanelMsg) {
	p := &m.historyPanel
	if !p.open || msg.generation != p.generation {
		return
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false
	p.err = msg.err
	p.report = nil
	p.markers = nil
	if msg.err != nil {
		return
	}
	if p.expectedPodUID != "" && msg.report.Selection.UID != p.expectedPodUID {
		p.err = fmt.Errorf("selected instance changed; close history and refresh the selection")
		return
	}
	if p.expectedContainerID != "" && (len(msg.report.Selection.Targets) != 1 || kube.NormalizeContainerID(msg.report.Selection.Targets[0].ContainerID) != p.expectedContainerID) {
		p.err = fmt.Errorf("selected instance changed; close history and refresh the selection")
		return
	}
	p.report = &msg.report
	p.markers = msg.markers
}

func (m *appModel) closeHistoryPanel() {
	if m.historyPanel.cancel != nil {
		m.historyPanel.cancel()
	}
	generation := m.historyPanel.generation + 1
	m.historyPanel = historyPanel{generation: generation}
}

func (m appModel) historyPanelKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := &m.historyPanel
	p.viewport.resize(max(1, m.bodyRows()-2))
	p.viewport.reconcile(len(m.historyPanelLines(m.layout().contentWidth())))
	switch msg.String() {
	case "esc", "H":
		m.closeHistoryPanel()
	case "q", "ctrl+c":
		m.closeHistoryPanel()
		m.cancelHistoryRequest()
		m.cancelNodeRequest()
		m.cancelVolumeRequest()
		return m, tea.Quit
	case "l":
		p.source = memoryhistory.Local
		command := m.fetchHistoryPanel()
		return m, command
	case "p":
		p.source = memoryhistory.Prometheus
		command := m.fetchHistoryPanel()
		return m, command
	case "m":
		if p.request.Scope == memoryhistory.Node {
			return m, nil
		}
		switch p.mode {
		case plainHistory:
			p.mode = markedHistory
		case markedHistory:
			p.mode = plainHistory
		}
		command := m.fetchHistoryPanel()
		return m, command
	case "r":
		command := m.fetchHistoryPanel()
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

package tui

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
)

type nodeReader interface {
	client.NodeEvidenceReader
	client.NodeHistoryReader
}

func (m appModel) nodeTarget() string {
	if m.restricted() {
		return ""
	}
	if m.view == viewDetail && m.detail.kind == entityNode {
		return m.detail.nodeName
	}
	if m.view == viewNodes {
		if ref, ok := m.currentEntityRef(); ok {
			return ref.nodeName
		}
	}
	return ""
}

func (m *appModel) ensureNodeTarget() tea.Cmd {
	name := m.nodeTarget()
	if name == m.selectedNode.name {
		return nil
	}
	m.cancelNodeRequest()
	m.selectedNode.selectName(name)
	return m.nodeRefreshCmd()
}

func (m *appModel) nodeRefreshCmd() tea.Cmd {
	if m.paused || m.selectedNode.name == "" {
		return nil
	}
	reader, ok := m.client.(nodeReader)
	if !ok {
		m.selectedNode.err = fmt.Errorf("Node context requires the authenticated Kubernetes API connection")
		return nil
	}
	request, ok := m.selectedNode.start()
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	m.nodeCancel = cancel
	return func() tea.Msg {
		defer cancel()
		msg := nodeMsg{request: request}
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			evidence, err := client.ReadNodeEvidence(ctx, reader, request.name, request.rank, nodeanalysis.DefaultContributors)
			msg.err = err
			if err == nil {
				msg.evidence = &evidence
			}
		}()
		go func() {
			defer wait.Done()
			history, err := client.ReadNodeHistory(ctx, reader, request.name)
			msg.historyErr = err
			if err == nil {
				msg.history = &history
			}
		}()
		wait.Wait()
		return msg
	}
}

func (m *appModel) cancelNodeRequest() {
	if m.nodeCancel != nil {
		m.nodeCancel()
		m.nodeCancel = nil
	}
	m.selectedNode.suspend()
}

func (m *appModel) clearNodeTarget() { m.cancelNodeRequest(); m.selectedNode.selectName("") }

func (m *appModel) cycleNodeRank() tea.Cmd {
	metrics := []nodeanalysis.Metric{nodeanalysis.Total, nodeanalysis.Anon, nodeanalysis.Cache, nodeanalysis.Shmem, nodeanalysis.Residual, nodeanalysis.PSI, nodeanalysis.OOM}
	for i, value := range metrics {
		if value == m.selectedNode.rank {
			m.selectedNode.rank = metrics[(i+1)%len(metrics)]
			break
		}
	}
	m.cancelNodeRequest()
	return m.nodeRefreshCmd()
}

func (m *appModel) receiveNode(msg nodeMsg) tea.Cmd {
	if !m.selectedNode.complete(msg, time.Now().UTC()) {
		return nil
	}
	if m.nodeCancel != nil {
		m.nodeCancel()
		m.nodeCancel = nil
	}
	if msg.err == nil && msg.evidence != nil && msg.evidence.Analysis.ContributorAccess == nodeanalysis.NodeOnly && m.opts.AllNamespaces {
		// A current denial of cluster-wide Pod reads invalidates cached cluster
		// contributors and any earlier in-flight response that could restore them.
		m.fetchGeneration++
		m.loading = false
		m.containerLoading = false
		m.data.Pods = nil
		m.data.Containers = nil
		m.data.Namespaces = nil
		m.data.Workloads = nil
		m.podTrends = nil
		m.action.compareSource = nil
		if m.action.overwriteRequest != nil && m.action.overwriteRequest.nodeReader == nil {
			m.action.overwriteRequest = nil
			m.action.result = actionResult{title: "Cluster Pod access unavailable"}
		}
		if m.action.pendingRequest != nil && m.action.pendingRequest.nodeReader == nil {
			m.action.activeID++
			m.action.pendingRequest = nil
			m.action.inFlight = false
			m.action.result = actionResult{title: "Cluster Pod access unavailable"}
		}
	}
	if m.view == viewDetail {
		m.syncDetailViewport()
	} else if m.layout().splitDetail {
		m.syncInlineDetailViewport()
	}
	return nil
}

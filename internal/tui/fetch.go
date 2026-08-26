package tui

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

func (m appModel) fetchCmd() tea.Cmd {
	generation := m.fetchGeneration
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()
		if reader, ok := m.client.(client.CurrentSummaryReader); ok {
			var current client.CurrentSummary
			var nodes []api.NodeSnapshotStatus
			var reliability api.DebugStore
			var currentErr, nodesErr, reliabilityErr error
			var wait sync.WaitGroup
			wait.Add(1)
			go func() { defer wait.Done(); current, currentErr = reader.CurrentSummary(ctx) }()
			if m.opts.AllNamespaces {
				wait.Add(2)
				go func() { defer wait.Done(); nodes, nodesErr = m.client.Nodes(ctx) }()
				go func() { defer wait.Done(); reliability, reliabilityErr = m.client.DebugStore(ctx) }()
			}
			wait.Wait()
			for _, err := range []error{currentErr, nodesErr, reliabilityErr} {
				if err != nil {
					return fetchMsg{generation: generation, err: err}
				}
			}
			data := snapshotData{
				Nodes: nodes, Namespaces: current.Namespaces, Workloads: current.Workloads,
				Pods: current.Pods, FetchedAt: time.Now().UTC(), Reliability: reliability.Reliability,
				ContainersLoaded: false,
			}
			data.Reliability = inferReliability(data)
			return fetchMsg{generation: generation, data: data}
		}
		if reader, ok := m.client.(client.CurrentSnapshotReader); ok {
			var current client.CurrentSnapshot
			var nodes []api.NodeSnapshotStatus
			var reliability api.DebugStore
			var currentErr, nodesErr, reliabilityErr error
			var wait sync.WaitGroup
			wait.Add(1)
			go func() { defer wait.Done(); current, currentErr = reader.CurrentSnapshot(ctx) }()
			if m.opts.AllNamespaces {
				wait.Add(2)
				go func() { defer wait.Done(); nodes, nodesErr = m.client.Nodes(ctx) }()
				go func() { defer wait.Done(); reliability, reliabilityErr = m.client.DebugStore(ctx) }()
			}
			wait.Wait()
			if currentErr != nil {
				return fetchMsg{generation: generation, err: currentErr}
			}
			if nodesErr != nil {
				return fetchMsg{generation: generation, err: nodesErr}
			}
			if reliabilityErr != nil {
				return fetchMsg{generation: generation, err: reliabilityErr}
			}
			data := snapshotData{
				Nodes: nodes, Namespaces: current.Namespaces, Workloads: current.Workloads,
				Pods: current.Pods, Containers: current.Containers, FetchedAt: time.Now().UTC(),
				Reliability: reliability.Reliability, ContainersLoaded: true,
			}
			data.Reliability = inferReliability(data)
			return fetchMsg{generation: generation, data: data}
		}
		var data snapshotData
		var namespaceErr, workloadErr, podErr, containerErr, nodeErr, reliabilityErr error
		var debug api.DebugStore
		var wait sync.WaitGroup
		wait.Add(4)
		if m.opts.AllNamespaces {
			wait.Add(2)
			go func() { defer wait.Done(); data.Nodes, nodeErr = m.client.Nodes(ctx) }()
			go func() { defer wait.Done(); debug, reliabilityErr = m.client.DebugStore(ctx) }()
		}
		go func() { defer wait.Done(); data.Namespaces, namespaceErr = m.client.Namespaces(ctx) }()
		go func() { defer wait.Done(); data.Workloads, workloadErr = m.client.Workloads(ctx) }()
		go func() { defer wait.Done(); data.Pods, podErr = m.client.Pods(ctx) }()
		go func() { defer wait.Done(); data.Containers, containerErr = m.client.Containers(ctx) }()
		wait.Wait()
		for _, err := range []error{nodeErr, reliabilityErr, namespaceErr, workloadErr, podErr, containerErr} {
			if err != nil {
				return fetchMsg{generation: generation, err: err}
			}
		}
		data.FetchedAt = time.Now().UTC()
		data.ContainersLoaded = true
		data.Reliability = debug.Reliability
		data.Reliability = inferReliability(data)
		return fetchMsg{generation: generation, data: data}
	}
}

func (m appModel) completeFetchCmd(generation uint64) tea.Cmd {
	reader, ok := m.client.(client.CurrentSnapshotReader)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
		defer cancel()
		data, err := reader.CurrentSnapshot(ctx)
		return completeFetchMsg{generation: generation, data: data, err: err}
	}
}

func (m *appModel) beginCompleteFetch() tea.Cmd {
	if m.loading || m.containerLoading {
		return nil
	}
	command := m.completeFetchCmd(m.fetchGeneration)
	if command != nil {
		m.containerLoading = true
	}
	return command
}

func (m appModel) requiresContainers() bool {
	return m.view == viewContainers || (m.view == viewDetail && (m.detail.kind == entityPod || m.detail.kind == entityContainer))
}

func inferReliability(data snapshotData) api.CollectorReliability {
	if data.Reliability.State != "" {
		return data.Reliability
	}
	result := api.CollectorReliability{State: api.CollectorRebuilding, Completeness: api.EvidencePartial}
	fresh, stale := 0, 0
	partial := false
	for _, pod := range data.Pods {
		if pod.Freshness == api.EvidenceFreshnessStale {
			stale++
		} else {
			fresh++
		}
		partial = partial || pod.Completeness == api.EvidencePartial
	}
	switch {
	case fresh > 0 && stale == 0 && !partial:
		result.State, result.Completeness = api.CollectorReady, api.EvidenceComplete
	case fresh > 0:
		result.State = api.CollectorDegraded
	case stale > 0:
		result.State = api.CollectorStale
	}
	return result
}

func (m *appModel) beginFetch() tea.Cmd {
	if m.loading || m.containerLoading {
		return nil
	}
	m.loading = true
	m.fetchGeneration++
	return m.fetchCmd()
}

func (m appModel) fetchHistoryCmd(request historyRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()
		series, err := m.client.PodHistory(ctx, request.namespace, request.podName)
		return historyMsg{namespace: request.namespace, podName: request.podName, generation: request.generation, series: series, err: err}
	}
}

func (m *appModel) historyRefreshCmd() tea.Cmd {
	request, ok := m.selectedHistory.start()
	if !ok {
		return nil
	}
	return m.fetchHistoryCmd(request)
}

func (m *appModel) ensureHistoryTarget() tea.Cmd {
	if m.view == viewDetail && (m.detail.kind == entityPod || m.detail.kind == entityContainer) {
		m.selectedHistory.selectPod(m.detail.namespace, m.detail.podName)
		return m.historyRefreshCmd()
	}
	if m.view == viewPods && m.layout().splitDetail {
		if pod, ok := m.selectedVisiblePod(); ok {
			m.selectedHistory.selectPod(pod.Namespace, pod.PodName)
			return m.historyRefreshCmd()
		}
	}
	m.selectedHistory.clearSelection()
	return nil
}

func (m appModel) tickCmd() tea.Cmd {
	interval := m.opts.RefreshInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

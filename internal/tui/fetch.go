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
		if reader, ok := m.client.(client.CurrentSnapshotReader); ok {
			var current client.CurrentSnapshot
			var nodes []api.NodeSnapshotStatus
			var currentErr, nodesErr error
			var wait sync.WaitGroup
			wait.Add(2)
			go func() { defer wait.Done(); current, currentErr = reader.CurrentSnapshot(ctx) }()
			go func() { defer wait.Done(); nodes, nodesErr = m.client.Nodes(ctx) }()
			wait.Wait()
			if currentErr != nil {
				return fetchMsg{generation: generation, err: currentErr}
			}
			if nodesErr != nil {
				return fetchMsg{generation: generation, err: nodesErr}
			}
			return fetchMsg{generation: generation, data: snapshotData{
				Nodes:      nodes,
				Namespaces: current.Namespaces,
				Workloads:  current.Workloads,
				Pods:       current.Pods, Containers: current.Containers,
				FetchedAt: time.Now().UTC(),
			}}
		}
		var data snapshotData
		var namespaceErr, workloadErr, podErr, containerErr, nodeErr error
		var wait sync.WaitGroup
		wait.Add(5)
		go func() { defer wait.Done(); data.Nodes, nodeErr = m.client.Nodes(ctx) }()
		go func() { defer wait.Done(); data.Namespaces, namespaceErr = m.client.Namespaces(ctx) }()
		go func() { defer wait.Done(); data.Workloads, workloadErr = m.client.Workloads(ctx) }()
		go func() { defer wait.Done(); data.Pods, podErr = m.client.Pods(ctx) }()
		go func() { defer wait.Done(); data.Containers, containerErr = m.client.Containers(ctx) }()
		wait.Wait()
		for _, err := range []error{nodeErr, namespaceErr, workloadErr, podErr, containerErr} {
			if err != nil {
				return fetchMsg{generation: generation, err: err}
			}
		}
		data.FetchedAt = time.Now().UTC()
		return fetchMsg{generation: generation, data: data}
	}
}

func (m *appModel) beginFetch() tea.Cmd {
	if m.loading {
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

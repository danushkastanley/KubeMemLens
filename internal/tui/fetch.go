package tui

import (
	"context"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

func (m appModel) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()
		if reader, ok := m.client.(client.CurrentSnapshotReader); ok {
			current, err := reader.CurrentSnapshot(ctx)
			if err != nil {
				return fetchMsg{err: err}
			}
			return fetchMsg{data: snapshotData{
				Namespaces: current.Namespaces,
				Workloads:  current.Workloads,
				Pods:       current.Pods, Containers: current.Containers,
				FetchedAt: time.Now().UTC(),
			}}
		}
		var data snapshotData
		var namespaceErr, workloadErr, podErr, containerErr error
		var wait sync.WaitGroup
		wait.Add(4)
		go func() { defer wait.Done(); data.Namespaces, namespaceErr = m.client.Namespaces(ctx) }()
		go func() { defer wait.Done(); data.Workloads, workloadErr = m.client.Workloads(ctx) }()
		go func() { defer wait.Done(); data.Pods, podErr = m.client.Pods(ctx) }()
		go func() { defer wait.Done(); data.Containers, containerErr = m.client.Containers(ctx) }()
		wait.Wait()
		for _, err := range []error{namespaceErr, workloadErr, podErr, containerErr} {
			if err != nil {
				return fetchMsg{err: err}
			}
		}
		data.FetchedAt = time.Now().UTC()
		return fetchMsg{data: data}
	}
}

func (m appModel) fetchHistoryCmd(namespace, podName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()
		series, err := m.client.PodHistory(ctx, namespace, podName)
		return historyMsg{namespace: namespace, podName: podName, series: series, err: err}
	}
}

func (m appModel) tickCmd() tea.Cmd {
	interval := m.opts.RefreshInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

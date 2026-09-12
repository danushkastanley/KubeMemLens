package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
	"github.com/danushkastanley/kube-memlens/internal/volumeview"
)

func (m *appModel) openVolumeDetail() tea.Cmd {
	if m.restricted() {
		return nil
	}
	if ref, ok := m.currentActionRef(); ok && ref.kind == entityWorkload {
		if m.view != viewDetail {
			m.detailParent = m.view
		}
		m.detail = ref
		m.detailSection = detailVolumes
		m.view = viewDetail
		m.resetCurrentViewport()
		return m.ensureHistoryTarget()
	}
	pod, ok := m.currentActionPod()
	if !ok {
		return nil
	}
	if m.view != viewDetail {
		m.detailParent = m.view
	}
	m.detail = entityRef{kind: entityPod, namespace: pod.Namespace, name: pod.PodName, podName: pod.PodName, nodeName: pod.NodeName}
	m.detailSection = detailVolumes
	m.view = viewDetail
	m.resetCurrentViewport()
	return tea.Batch(m.ensureHistoryTarget(), m.beginCompleteFetch())
}

func (m appModel) volumeDetailLines(width int) []string {
	s := m.selectedVolumes
	if m.detail.kind == entityWorkload {
		return m.workloadVolumeDetailLines(width)
	}
	if _, ok := m.client.(client.PodVolumeReader); !ok {
		return nodeview.Wrap([]string{"Volume context requires the authenticated Kubernetes API connection and optional volume profile."}, width)
	}
	lines := []string{"Volume context | v volumes | e memory | h back | C capture | y copy command", m.volumeRefreshLabel()}
	if s.inFlight {
		lines = append(lines, "Refreshing authorised volume context...")
	}
	if s.err != nil {
		label := "Volume source unavailable; retained evidence keeps its original sample times."
		if client.IsForbidden(s.err) {
			label = "Volume context access denied; protected volume evidence and actions cleared."
		}
		if client.IsNotFound(s.err) {
			label = "Volume profile or selected Pod was not found; no volume evidence is retained."
		}
		lines = append(lines, label)
	}
	if s.context == nil {
		return nodeview.Wrap(append(lines, "No authorised volume evidence available."), width)
	}
	result := explain.AnalyzeVolumes(explain.VolumeInput{Pod: s.pod, Volumes: s.context.Context, Previous: s.previous, Now: time.Now().UTC()})
	return append(nodeview.Wrap(lines, width), volumeview.Lines(s.context.Context, result, time.Now().UTC(), width)...)
}

func (m appModel) volumeRefreshLabel() string {
	if m.paused {
		return "Volume refresh paused; retained sample timestamps continue ageing."
	}
	return "Volume refresh automatic: at most once per 15 seconds; memory sampling is independent."
}

func (m appModel) volumeSummaryLines(pod api.PodSnapshot, width int) []string {
	s := m.selectedVolumes
	if s.namespace != pod.Namespace || s.name != pod.PodName || s.uid != pod.PodUID {
		return nil
	}
	if client.IsNotFound(s.err) {
		return nil
	}
	if client.IsForbidden(s.err) {
		return []string{"Volume context: access denied."}
	}
	if s.context == nil {
		return nil
	}
	r := explain.AnalyzeVolumes(explain.VolumeInput{Pod: pod, Volumes: s.context.Context, Previous: s.previous, Now: time.Now().UTC()})
	return nodeview.Wrap([]string{fmt.Sprintf("Volumes: %d configured | storage severity %s | v opens volume context", len(s.context.Context.Volumes), r.StorageSeverity), "Filesystem bytes remain outside memory totals."}, width)
}

func (m appModel) volumeCommand() (string, bool) {
	s := m.selectedVolumes
	if (s.context == nil && s.workload == nil) || s.err != nil {
		return "", false
	}
	command := "kubectl memlens volumes pod " + volumeview.Quote(s.name) + " -n " + volumeview.Quote(s.namespace)
	if s.kind != "" {
		command = "kubectl memlens volumes workload " + volumeview.Quote(s.kind+"/"+s.name) + " -n " + volumeview.Quote(s.namespace)
	}
	for _, option := range []struct{ flag, value string }{{"--kubeconfig", m.opts.ConnectionOptions.Kubeconfig}, {"--context", m.opts.ConnectionOptions.Context}} {
		if option.value != "" {
			command += " " + option.flag + " " + volumeview.Quote(option.value)
		}
	}
	return command, true
}

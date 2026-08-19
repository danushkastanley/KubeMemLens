package tui

import (
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

func (m appModel) activeNamespace() (string, bool) {
	if m.currentNamespace != "" {
		return m.currentNamespace, false
	}
	if !m.opts.AllNamespaces && m.opts.Namespace != "" {
		return m.opts.Namespace, false
	}
	return "", true
}

func (m appModel) visibleNamespaces() []api.NamespaceSnapshot {
	namespace, all := m.activeNamespace()
	items := FilterNamespaces(m.data.Namespaces, namespace, all, m.query)
	SortNamespaces(items, m.sort)
	return items
}

func (m appModel) visibleNodes() []nodeView {
	return buildNodeViews(m.data.Nodes, m.data.Pods, m.query)
}

func (m appModel) visiblePods() []api.PodSnapshot {
	namespace, all := m.activeNamespace()
	items := FilterPodsAt(m.data.Pods, namespace, all, m.query, m.riskNow(), m.staleAfter())
	if m.currentNode != "" {
		filtered := make([]api.PodSnapshot, 0, len(items))
		for _, pod := range items {
			if pod.NodeName == m.currentNode {
				filtered = append(filtered, pod)
			}
		}
		items = filtered
	}
	if m.currentWorkloadName != "" {
		filtered := make([]api.PodSnapshot, 0, len(items))
		for _, pod := range items {
			if pod.Context.WorkloadKind == m.currentWorkloadKind && pod.Context.WorkloadName == m.currentWorkloadName {
				filtered = append(filtered, pod)
			}
		}
		items = filtered
	}
	SortPodsAt(items, m.sort, m.riskNow(), m.staleAfter())
	return items
}

func (m appModel) visibleWorkloads() []api.WorkloadSnapshot {
	namespace, all := m.activeNamespace()
	items := FilterWorkloads(m.data.Workloads, namespace, all, m.query)
	SortWorkloads(items, m.sort)
	return items
}

func (m appModel) visibleContainers() []api.ContainerSnapshot {
	namespace, all := m.activeNamespace()
	items := FilterContainers(m.data.Containers, namespace, all, m.query)
	if m.currentNode != "" {
		filtered := make([]api.ContainerSnapshot, 0, len(items))
		for _, container := range items {
			if container.NodeName == m.currentNode {
				filtered = append(filtered, container)
			}
		}
		items = filtered
	}
	if m.currentWorkloadName != "" {
		filtered := make([]api.ContainerSnapshot, 0, len(items))
		for _, container := range items {
			if container.Context.WorkloadKind == m.currentWorkloadKind && container.Context.WorkloadName == m.currentWorkloadName {
				filtered = append(filtered, container)
			}
		}
		items = filtered
	}
	SortContainers(items, m.sort)
	return items
}

func (m appModel) selectedPod() (api.PodSnapshot, bool) {
	for _, pod := range m.data.Pods {
		if pod.Namespace == m.selectedPodNS && pod.PodName == m.selectedPodName {
			return pod, true
		}
	}
	return api.PodSnapshot{}, false
}

func statusError(opts client.Options, description string, err error) string {
	return client.ConnectionError(opts, description, err).Error()
}

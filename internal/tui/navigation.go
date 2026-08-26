package tui

import tea "charm.land/bubbletea/v2"

func (m *appModel) enter() tea.Cmd {
	switch m.view {
	case viewNodes:
		items := m.visibleNodes()
		if len(items) == 0 {
			return nil
		}
		m.currentNode = items[m.currentViewport().selected].name
		m.view = viewPods
		m.resetCurrentViewport()
	case viewNamespaces:
		items := m.visibleNamespaces()
		if len(items) == 0 {
			return nil
		}
		m.currentNamespace = items[m.currentViewport().selected].Namespace
		m.view = viewPods
		m.resetCurrentViewport()
	case viewWorkloads:
		items := m.visibleWorkloads()
		if len(items) == 0 {
			return nil
		}
		selected := m.currentViewport().selected
		m.currentNamespace = items[selected].Namespace
		m.currentWorkloadKind = items[selected].Kind
		m.currentWorkloadName = items[selected].Name
		m.view = viewPods
		m.resetCurrentViewport()
	case viewPods, viewContainers:
		return m.openSelectedDetail()
	}
	return nil
}

func (m *appModel) back() {
	switch m.view {
	case viewDetail:
		m.view = m.detailParent
	case viewContainers:
		m.view = viewPods
		m.resetCurrentViewport()
	case viewPods:
		if m.currentNode != "" {
			m.currentNode = ""
			m.view = viewNodes
			m.resetCurrentViewport()
		} else if m.currentWorkloadName != "" {
			m.currentWorkloadKind = ""
			m.currentWorkloadName = ""
			m.view = viewWorkloads
			m.resetCurrentViewport()
		} else if m.currentNamespace != "" {
			m.currentNamespace = ""
			m.view = viewNamespaces
			m.resetCurrentViewport()
		}
	case viewWorkloads:
		m.currentNamespace = ""
		m.view = viewNamespaces
		m.resetCurrentViewport()
	default:
		m.view = viewPods
	}
	m.reconcileCurrentViewport("")
}

func (m *appModel) move(delta int) {
	if m.view == viewDetail {
		m.syncDetailViewport()
	} else if m.focus == focusDetail && m.layout().splitDetail {
		m.syncInlineDetailViewport()
	}
	m.activeViewport().move(delta)
	if m.focus == focusTable && m.view != viewDetail {
		m.viewports[viewDetail].reset()
	}
}

func (m *appModel) currentViewport() *viewport {
	return &m.viewports[m.view]
}

func (m *appModel) activeViewport() *viewport {
	if m.view != viewDetail && m.focus == focusDetail && m.layout().splitDetail {
		return &m.viewports[viewDetail]
	}
	return m.currentViewport()
}

func (m *appModel) resetCurrentViewport() {
	m.currentViewport().reconcile(m.visibleCount())
	m.currentViewport().reset()
}

func (m *appModel) reconcileCurrentViewport(preferredKey string) {
	viewport := m.currentViewport()
	viewport.reconcile(m.visibleCount())
	if preferredKey == "" || m.view == viewDetail {
		return
	}
	for index, key := range m.visibleEntityKeys() {
		if key == preferredKey {
			viewport.selected = index
			viewport.reconcile(viewport.count)
			return
		}
	}
}

func (m *appModel) resizeViewports() {
	plan := m.layout()
	capacity := plan.tableRows()
	if capacity < 1 {
		capacity = 1
	}
	for view := viewNodes; view < viewDetail; view++ {
		m.viewports[view].resize(capacity)
	}
	m.viewports[viewDetail].resize(plan.bodyRows)
}

func (m appModel) layout() layoutPlan {
	return layoutFor(m.width, m.height, m.view)
}

func (m *appModel) syncDetailViewport() {
	width := m.width
	if width <= 0 {
		width = 100
	}
	m.viewports[viewDetail].reconcile(len(m.detailLines(width)))
}

func (m *appModel) syncInlineDetailViewport() {
	plan := m.layout()
	m.viewports[viewDetail].reconcile(len(m.inlineDetailLines(plan.detailWidth)))
}

func (m appModel) visibleCount() int {
	switch m.view {
	case viewNodes:
		return len(m.visibleNodes())
	case viewNamespaces:
		return len(m.visibleNamespaces())
	case viewPods:
		return len(m.visiblePods())
	case viewWorkloads:
		return len(m.visibleWorkloads())
	case viewContainers:
		return len(m.visibleContainers())
	case viewDetail:
		width := m.width
		if width <= 0 {
			width = 100
		}
		return len(m.detailLines(width))
	default:
		return 0
	}
}

func (m *appModel) openSelectedDetail() tea.Cmd {
	ref, ok := m.currentEntityRef()
	if !ok {
		return nil
	}
	m.detail = ref
	m.detailParent = m.view
	m.selectedPodNS = ref.namespace
	m.selectedPodName = ref.podName
	m.view = viewDetail
	m.resetCurrentViewport()
	if ref.kind == entityPod || ref.kind == entityContainer {
		m.selectedHistory.selectPod(ref.namespace, ref.podName)
		return tea.Batch(m.historyRefreshCmd(), m.beginCompleteFetch())
	}
	m.selectedHistory.clearSelection()
	return nil
}

func (m appModel) currentEntityRef() (entityRef, bool) {
	selected := m.currentViewport().selected
	switch m.view {
	case viewNodes:
		items := m.visibleNodes()
		if selected < len(items) {
			return entityRef{kind: entityNode, name: items[selected].name, nodeName: items[selected].name}, true
		}
	case viewNamespaces:
		items := m.visibleNamespaces()
		if selected < len(items) {
			return entityRef{kind: entityNamespace, namespace: items[selected].Namespace, name: items[selected].Namespace}, true
		}
	case viewWorkloads:
		items := m.visibleWorkloads()
		if selected < len(items) {
			item := items[selected]
			return entityRef{kind: entityWorkload, namespace: item.Namespace, name: item.Name, workloadKind: item.Kind}, true
		}
	case viewPods:
		items := m.visiblePods()
		if selected < len(items) {
			item := items[selected]
			return entityRef{kind: entityPod, namespace: item.Namespace, name: item.PodName, podName: item.PodName, nodeName: item.NodeName}, true
		}
	case viewContainers:
		items := m.visibleContainers()
		if selected < len(items) {
			item := items[selected]
			return entityRef{kind: entityContainer, namespace: item.Namespace, name: item.ContainerName, podName: item.PodName, containerName: item.ContainerName, nodeName: item.NodeName}, true
		}
	}
	return entityRef{}, false
}

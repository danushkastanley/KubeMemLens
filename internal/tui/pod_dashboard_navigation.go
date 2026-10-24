package tui

import tea "charm.land/bubbletea/v2"

type podPanel int

const (
	podComposition podPanel = iota
	podContext
	podHistory
	podChecks
	podContainers
	podSignals
	podPanelCount
)

var podPanelTitles = [...]string{"WHERE MEMORY GOES", "POD CONTEXT", "RECENT POD HISTORY", "EXPLANATION & NEXT CHECKS", "CONTAINERS", "RECENT EVENTS & PRESSURE"}

type podDashboardState struct {
	panel           podPanel
	panels          [podPanelCount]viewport
	actionIndex     int
	containerIndex  int
	raw             bool
	containerDetail bool
	parentKey       string
	parentViewport  viewport
	parentDetail    viewport
	parentFocus     focusMode
	parentQuery     string
	parentSort      sortMode
	parentView      viewMode
	podRef          entityRef
}

func (m appModel) isPodDashboard() bool {
	return !m.restricted() && m.view == viewDetail && m.detail.kind == entityPod && m.detailSection == detailMemory && !m.podDashboard.raw
}

func (m *appModel) rememberPodDashboard(ref entityRef) {
	m.podDashboard = podDashboardState{
		parentKey: m.selectedEntityKey(), parentViewport: *m.currentViewport(),
		parentDetail: m.viewports[viewDetail], parentFocus: m.focus,
		parentQuery: m.query, parentSort: m.sort, parentView: m.view, podRef: ref,
	}
}

// Return by identity: a refresh may have reordered the table while it was hidden.
func (m *appModel) returnFromPodDashboard() bool {
	s := &m.podDashboard
	if m.view != viewDetail || s.podRef.kind != entityPod {
		return false
	}
	if s.containerDetail {
		m.detail = s.podRef
		m.detailParent = s.parentView
		s.containerDetail = false
		return true
	}
	if m.detailSection == detailVolumes && m.detail.kind == entityPod {
		m.detailSection = detailMemory
		return true
	}
	if s.raw {
		s.raw = false
		return true
	}
	if m.detail.kind != entityPod {
		return false
	}
	m.view, m.query, m.sort, m.focus = s.parentView, s.parentQuery, s.parentSort, s.parentFocus
	m.viewports[m.view] = s.parentViewport
	m.viewports[m.view].resize(m.layout().tableRows())
	m.viewports[viewDetail] = s.parentDetail
	m.reconcileCurrentViewport(s.parentKey)
	*s = podDashboardState{}
	return true
}

func (m *appModel) podDashboardKey(key string) (bool, tea.Cmd) {
	s := &m.podDashboard
	switch key {
	case "tab", "shift+tab":
		step := podPanel(1)
		if key == "shift+tab" {
			step = podPanelCount - 1
		}
		s.panel = (s.panel + step) % podPanelCount
	case "esc", "h", "backspace":
		m.back()
		return true, m.ensureHistoryTarget()
	case "e":
		s.raw = true
		m.resetCurrentViewport()
	case "enter":
		return true, m.openPodDashboardItem()
	case "j", "down", "k", "up", "pgdown", "pgup", "g", "G":
		m.movePodDashboard(key)
	case "/", "s":
		// Filtering and sorting belong to the parent table, which remains intact.
		return true, nil
	default:
		return false, nil
	}
	return true, nil
}

func (m *appModel) movePodDashboard(key string) {
	s := &m.podDashboard
	box := m.podDashboardBoxes()[s.panel]
	pod, ok := m.findPod(m.detail.namespace, m.detail.podName)
	if !ok {
		return
	}
	v := &s.panels[s.panel]
	v.resize(maxInt(1, box.height-4))
	v.reconcile(len(m.podPanelLines(pod, s.panel, box.width-2)))
	step := 1
	if key == "k" || key == "up" || key == "pgup" {
		step = -1
	}
	switch s.panel {
	case podChecks:
		s.actionIndex = minInt(2, maxInt(0, s.actionIndex+step))
	case podContainers:
		s.containerIndex = minInt(maxInt(0, len(pod.Containers)-1), maxInt(0, s.containerIndex+step))
	}
	switch key {
	case "g":
		v.first()
		s.actionIndex, s.containerIndex = 0, 0
	case "G":
		v.last()
		s.actionIndex, s.containerIndex = 2, maxInt(0, len(pod.Containers)-1)
	case "pgup", "pgdown":
		v.move(step * v.capacity)
	default:
		v.move(step)
	}
}

func (m *appModel) openPodDashboardItem() tea.Cmd {
	s := &m.podDashboard
	switch s.panel {
	case podChecks:
		switch s.actionIndex {
		case 0:
			return m.startRecommendation()
		case 1:
			return m.startCompare()
		case 2:
			s.raw = true
			m.resetCurrentViewport()
		}
	case podContainers:
		pod, ok := m.findPod(m.detail.namespace, m.detail.podName)
		if !ok || len(pod.Containers) == 0 {
			return nil
		}
		index := minInt(s.containerIndex, len(pod.Containers)-1)
		c := pod.Containers[index]
		m.detail = entityRef{kind: entityContainer, namespace: pod.Namespace, podName: pod.PodName, containerName: c.ContainerName, name: c.ContainerName, nodeName: pod.NodeName}
		s.containerDetail = true
		m.resetCurrentViewport()
	}
	return nil
}

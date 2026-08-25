package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

type appModel struct {
	ctx                   context.Context
	client                client.SnapshotReader
	connectionDescription string
	opts                  Options
	view                  viewMode
	sort                  sortMode
	query                 string
	searching             bool
	help                  bool
	paused                bool
	focus                 focusMode
	width                 int
	height                int
	viewports             [viewModeCount]viewport
	action                actionState
	actionExecutor        actionExecutor

	currentNamespace    string
	currentNode         string
	currentWorkloadKind string
	currentWorkloadName string
	selectedPodNS       string
	selectedPodName     string
	detail              entityRef
	detailParent        viewMode

	data            snapshotData
	podTrends       map[string]int8
	lastRefresh     time.Time
	statusErr       error
	loading         bool
	fetchGeneration uint64
	selectedHistory selectedHistory
}

type fetchMsg struct {
	generation uint64
	data       snapshotData
	err        error
}

type tickMsg time.Time

type historyMsg struct {
	namespace  string
	podName    string
	generation uint64
	series     []api.PodHistory
	err        error
}

func newModel(ctx context.Context, opts Options, reader client.SnapshotReader, description string) appModel {
	m := appModel{
		ctx:                   ctx,
		client:                reader,
		connectionDescription: description,
		opts:                  opts,
		view:                  viewPods,
		sort:                  sortRisk,
		loading:               true,
		fetchGeneration:       1,
		podTrends:             make(map[string]int8),
		actionExecutor:        localActionExecutor{},
	}
	m.resizeViewports()
	return m
}

func (m appModel) Init() tea.Cmd {
	return tea.Batch(m.fetchCmd(), m.tickCmd())
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewports()
		if !m.layout().splitDetail {
			m.focus = focusTable
		}
		m.reconcileCurrentViewport("")
		return m, m.ensureHistoryTarget()
	case tickMsg:
		if m.paused {
			return m, m.tickCmd()
		}
		return m, tea.Batch(m.beginFetch(), m.historyRefreshCmd(), m.tickCmd())
	case fetchMsg:
		if msg.generation != 0 && msg.generation != m.fetchGeneration {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			if client.IsForbidden(msg.err) {
				m.clearRevokedData()
			}
			m.statusErr = msg.err
			return m, nil
		}
		selectedKey := m.selectedEntityKey()
		m.updatePodTrends(msg.data.Pods)
		m.data = msg.data
		m.lastRefresh = time.Now()
		m.statusErr = nil
		m.reconcileCurrentViewport(selectedKey)
		return m, m.ensureHistoryTarget()
	case historyMsg:
		if m.selectedHistory.complete(msg, time.Now()) {
			m.syncDetailViewport()
		}
		return m, nil
	case actionMsg:
		m.completeAction(msg)
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	default:
		return m, nil
	}
}

func (m *appModel) clearRevokedData() {
	m.data = snapshotData{}
	m.podTrends = make(map[string]int8)
	m.lastRefresh = time.Time{}
	m.selectedHistory.clearSelection()
	m.action = actionState{}
	m.currentNamespace = ""
	m.currentNode = ""
	m.currentWorkloadKind = ""
	m.currentWorkloadName = ""
	m.selectedPodNS = ""
	m.selectedPodName = ""
	m.detail = entityRef{}
}

func (m *appModel) updatePodTrends(next []api.PodSnapshot) {
	previous := make(map[string]uint64, len(m.data.Pods))
	for _, pod := range m.data.Pods {
		previous[podKey(pod.Namespace, pod.PodName)] = pod.Memory.TotalBytes
	}
	trends := make(map[string]int8, len(next))
	for _, pod := range next {
		key := podKey(pod.Namespace, pod.PodName)
		before, ok := previous[key]
		if !ok || pod.Memory.TotalBytes == before {
			trends[key] = 0
		} else if pod.Memory.TotalBytes > before {
			trends[key] = 1
		} else {
			trends[key] = -1
		}
	}
	m.podTrends = trends
}

func (m appModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.action.mode != actionClosed {
		return m.handleActionKey(msg)
	}
	if m.searching {
		switch msg.String() {
		case "esc":
			m.searching = false
			m.query = ""
			m.resetCurrentViewport()
		case "enter":
			m.searching = false
		case "backspace":
			if len(m.query) > 0 {
				runes := []rune(m.query)
				m.query = string(runes[:len(runes)-1])
				m.resetCurrentViewport()
			}
		default:
			if msg.Key().Text != "" {
				m.query += msg.Key().Text
				m.resetCurrentViewport()
			}
		}
		return m, m.ensureHistoryTarget()
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help = !m.help
	case "a":
		m.action.mode = actionMenu
		m.action.err = nil
	case "R":
		return m, m.startRecommendation()
	case "x":
		return m, m.startCompare()
	case "C":
		m.action.mode = actionCapturePath
		m.action.input = ""
		m.action.err = nil
	case "y":
		return m.copyCurrentCommand()
	case "r":
		return m, tea.Batch(m.beginFetch(), m.historyRefreshCmd())
	case "/":
		m.searching = true
	case "space":
		m.paused = !m.paused
	case "esc":
		if m.query != "" {
			m.query = ""
		} else if m.view == viewDetail {
			m.back()
		}
		m.reconcileCurrentViewport("")
	case "tab":
		if m.layout().splitDetail {
			if m.focus == focusTable {
				m.focus = focusDetail
				m.syncInlineDetailViewport()
			} else {
				m.focus = focusTable
			}
		}
	case "n":
		m.view = viewNamespaces
		m.currentNode = ""
		m.currentNamespace = ""
		m.currentWorkloadKind = ""
		m.currentWorkloadName = ""
		m.resetCurrentViewport()
	case "N":
		m.view = viewNodes
		m.currentNamespace = ""
		m.currentNode = ""
		m.currentWorkloadKind = ""
		m.currentWorkloadName = ""
		m.resetCurrentViewport()
	case "w":
		m.view = viewWorkloads
		m.currentWorkloadKind = ""
		m.currentWorkloadName = ""
		m.resetCurrentViewport()
	case "p":
		m.view = viewPods
		m.currentWorkloadKind = ""
		m.currentWorkloadName = ""
		m.resetCurrentViewport()
	case "c":
		m.view = viewContainers
		m.resetCurrentViewport()
	case "e":
		return m, m.openSelectedDetail()
	case "enter":
		return m, m.enter()
	case "backspace", "h":
		m.back()
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-m.activeViewport().capacity)
	case "pgdown":
		m.move(m.activeViewport().capacity)
	case "g":
		m.activeViewport().first()
	case "G":
		m.activeViewport().last()
	case "s":
		selectedKey := m.selectedEntityKey()
		m.sort = nextSort(m.sort)
		m.reconcileCurrentViewport(selectedKey)
	}
	return m, m.ensureHistoryTarget()
}

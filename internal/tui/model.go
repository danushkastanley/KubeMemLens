package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	selected              int
	width                 int
	height                int

	currentNamespace    string
	currentWorkloadKind string
	currentWorkloadName string
	selectedPodNS       string
	selectedPodName     string

	data           snapshotData
	lastRefresh    time.Time
	statusErr      error
	loading        bool
	history        []api.PodHistory
	historyLoading bool
	historyErr     error
}

type fetchMsg struct {
	data snapshotData
	err  error
}

type tickMsg time.Time

type historyMsg struct {
	namespace string
	podName   string
	series    []api.PodHistory
	err       error
}

func newModel(ctx context.Context, opts Options, reader client.SnapshotReader, description string) appModel {
	return appModel{
		ctx:                   ctx,
		client:                reader,
		connectionDescription: description,
		opts:                  opts,
		view:                  viewNamespaces,
		sort:                  sortTotal,
		loading:               true,
	}
}

func (m appModel) Init() tea.Cmd {
	return tea.Batch(m.fetchCmd(), m.tickCmd())
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tickMsg:
		if m.paused {
			return m, m.tickCmd()
		}
		m.loading = true
		return m, tea.Batch(m.fetchCmd(), m.tickCmd())
	case fetchMsg:
		m.loading = false
		if msg.err != nil {
			m.statusErr = msg.err
			return m, nil
		}
		m.data = msg.data
		m.lastRefresh = time.Now()
		m.statusErr = nil
		m.clampSelection()
		return m, nil
	case historyMsg:
		if msg.namespace == m.selectedPodNS && msg.podName == m.selectedPodName {
			m.history, m.historyErr, m.historyLoading = msg.series, msg.err, false
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	default:
		return m, nil
	}
}

func (m appModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searching {
		switch msg.String() {
		case "esc":
			m.searching = false
			m.query = ""
			m.clampSelection()
		case "enter":
			m.searching = false
		case "backspace":
			if len(m.query) > 0 {
				runes := []rune(m.query)
				m.query = string(runes[:len(runes)-1])
				m.clampSelection()
			}
		default:
			if len(msg.Runes) > 0 {
				m.query += string(msg.Runes)
				m.selected = 0
				m.clampSelection()
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help = !m.help
	case "r":
		m.loading = true
		return m, m.fetchCmd()
	case "/":
		m.searching = true
	case " ":
		m.paused = !m.paused
	case "esc":
		if m.query != "" {
			m.query = ""
		} else if m.view == viewDetail {
			m.view = viewPods
		}
		m.clampSelection()
	case "tab":
		m.cycleView()
	case "n":
		m.view = viewNamespaces
		m.currentNamespace = ""
		m.currentWorkloadKind = ""
		m.currentWorkloadName = ""
		m.selected = 0
	case "w":
		m.view = viewWorkloads
		m.currentWorkloadKind = ""
		m.currentWorkloadName = ""
		m.selected = 0
	case "p":
		m.view = viewPods
		m.currentWorkloadKind = ""
		m.currentWorkloadName = ""
		m.selected = 0
	case "c":
		m.view = viewContainers
		m.selected = 0
	case "e":
		return m, m.openSelectedPodDetail()
	case "enter":
		return m, m.enter()
	case "backspace", "h":
		m.back()
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-10)
	case "pgdown":
		m.move(10)
	case "g":
		m.selected = 0
	case "G":
		m.selected = m.visibleCount() - 1
		m.clampSelection()
	case "s":
		m.sort = nextSort(m.sort)
		m.selected = 0
	}
	return m, nil
}

func (m *appModel) cycleView() {
	switch m.view {
	case viewNamespaces:
		m.view = viewWorkloads
	case viewWorkloads:
		m.view = viewPods
	case viewPods:
		m.view = viewContainers
	case viewContainers:
		m.view = viewNamespaces
	default:
		m.view = viewNamespaces
	}
	m.selected = 0
	m.clampSelection()
}

func (m *appModel) enter() tea.Cmd {
	switch m.view {
	case viewNamespaces:
		items := m.visibleNamespaces()
		if len(items) == 0 {
			return nil
		}
		m.currentNamespace = items[m.selected].Namespace
		m.view = viewPods
		m.selected = 0
	case viewWorkloads:
		items := m.visibleWorkloads()
		if len(items) == 0 {
			return nil
		}
		m.currentNamespace = items[m.selected].Namespace
		m.currentWorkloadKind = items[m.selected].Kind
		m.currentWorkloadName = items[m.selected].Name
		m.view = viewPods
		m.selected = 0
	case viewPods:
		return m.openSelectedPodDetail()
	case viewContainers:
		return m.openSelectedPodDetail()
	}
	return nil
}

func (m *appModel) back() {
	switch m.view {
	case viewDetail:
		m.view = viewPods
	case viewPods, viewContainers:
		if m.currentWorkloadName != "" {
			m.currentWorkloadKind = ""
			m.currentWorkloadName = ""
			m.view = viewWorkloads
			m.selected = 0
		} else if m.currentNamespace != "" {
			m.currentNamespace = ""
			m.view = viewNamespaces
			m.selected = 0
		}
	case viewWorkloads:
		m.currentNamespace = ""
		m.view = viewNamespaces
		m.selected = 0
	default:
		m.view = viewNamespaces
	}
	m.clampSelection()
}

func (m *appModel) move(delta int) {
	m.selected += delta
	m.clampSelection()
}

func (m *appModel) clampSelection() {
	count := m.visibleCount()
	if count == 0 {
		m.selected = 0
		return
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= count {
		m.selected = count - 1
	}
}

func (m appModel) visibleCount() int {
	switch m.view {
	case viewNamespaces:
		return len(m.visibleNamespaces())
	case viewPods:
		return len(m.visiblePods())
	case viewWorkloads:
		return len(m.visibleWorkloads())
	case viewContainers:
		return len(m.visibleContainers())
	default:
		return 1
	}
}

func (m *appModel) openSelectedPodDetail() tea.Cmd {
	var ns, name string
	switch m.view {
	case viewPods:
		items := m.visiblePods()
		if len(items) == 0 {
			return nil
		}
		ns, name = items[m.selected].Namespace, items[m.selected].PodName
	case viewContainers:
		items := m.visibleContainers()
		if len(items) == 0 {
			return nil
		}
		ns, name = items[m.selected].Namespace, items[m.selected].PodName
	default:
		return nil
	}
	m.selectedPodNS = ns
	m.selectedPodName = name
	m.view = viewDetail
	m.history = nil
	m.historyErr = nil
	m.historyLoading = true
	return m.fetchHistoryCmd(ns, name)
}

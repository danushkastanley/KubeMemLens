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
	selected              int
	width                 int
	height                int

	currentNamespace string
	selectedPodNS    string
	selectedPodName  string

	data        snapshotData
	lastRefresh time.Time
	statusErr   error
	loading     bool
}

type fetchMsg struct {
	data snapshotData
	err  error
}

type tickMsg time.Time

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
		m.selected = 0
	case "p":
		m.view = viewPods
		m.selected = 0
	case "c":
		m.view = viewContainers
		m.selected = 0
	case "e":
		m.openSelectedPodDetail()
	case "enter":
		m.enter()
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
	case "s":
		m.sort = nextSort(m.sort)
		m.selected = 0
	}
	return m, nil
}

func (m appModel) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()

		namespaces, err := m.client.Namespaces(ctx)
		if err != nil {
			return fetchMsg{err: err}
		}
		pods, err := m.client.Pods(ctx)
		if err != nil {
			return fetchMsg{err: err}
		}
		containers, err := m.client.Containers(ctx)
		if err != nil {
			return fetchMsg{err: err}
		}

		return fetchMsg{data: snapshotData{
			Namespaces: namespaces,
			Pods:       pods,
			Containers: containers,
			FetchedAt:  time.Now().UTC(),
		}}
	}
}

func (m appModel) tickCmd() tea.Cmd {
	interval := m.opts.RefreshInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *appModel) cycleView() {
	switch m.view {
	case viewNamespaces:
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

func (m *appModel) enter() {
	switch m.view {
	case viewNamespaces:
		items := m.visibleNamespaces()
		if len(items) == 0 {
			return
		}
		m.currentNamespace = items[m.selected].Namespace
		m.view = viewPods
		m.selected = 0
	case viewPods:
		m.openSelectedPodDetail()
	case viewContainers:
		m.openSelectedPodDetail()
	}
}

func (m *appModel) back() {
	switch m.view {
	case viewDetail:
		m.view = viewPods
	case viewPods, viewContainers:
		if m.currentNamespace != "" {
			m.currentNamespace = ""
			m.view = viewNamespaces
			m.selected = 0
		}
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
	case viewContainers:
		return len(m.visibleContainers())
	default:
		return 1
	}
}

func (m *appModel) openSelectedPodDetail() {
	var ns, name string
	switch m.view {
	case viewPods:
		items := m.visiblePods()
		if len(items) == 0 {
			return
		}
		ns, name = items[m.selected].Namespace, items[m.selected].PodName
	case viewContainers:
		items := m.visibleContainers()
		if len(items) == 0 {
			return
		}
		ns, name = items[m.selected].Namespace, items[m.selected].PodName
	default:
		return
	}
	m.selectedPodNS = ns
	m.selectedPodName = name
	m.view = viewDetail
}

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

func (m appModel) visiblePods() []api.PodSnapshot {
	namespace, all := m.activeNamespace()
	items := FilterPods(m.data.Pods, namespace, all, m.query)
	SortPods(items, m.sort)
	return items
}

func (m appModel) visibleContainers() []api.ContainerSnapshot {
	namespace, all := m.activeNamespace()
	items := FilterContainers(m.data.Containers, namespace, all, m.query)
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

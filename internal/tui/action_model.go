package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
)

type actionMode int

const (
	actionClosed actionMode = iota
	actionMenu
	actionCapturePath
	actionResultMode
)

type actionState struct {
	mode             actionMode
	input            string
	result           actionResult
	err              error
	inFlight         bool
	nextID           uint64
	activeID         uint64
	compareSource    *api.PodSnapshot
	overwriteRequest *actionRequest
}

type actionMsg struct {
	id     uint64
	result actionResult
	err    error
}

func (m appModel) handleActionKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := message.String()
	switch m.action.mode {
	case actionMenu:
		switch key {
		case "esc", "a":
			m.action.mode = actionClosed
		case "r":
			return m, m.startRecommendation()
		case "x":
			return m, m.startCompare()
		case "c":
			m.action.mode = actionCapturePath
			m.action.input = ""
			m.action.err = nil
		case "y":
			return m.copyCurrentCommand()
		}
	case actionCapturePath:
		switch key {
		case "esc":
			m.action.mode = actionClosed
		case "enter":
			return m, m.startCapture(false)
		case "backspace":
			runes := []rune(m.action.input)
			if len(runes) > 0 {
				m.action.input = string(runes[:len(runes)-1])
			}
		default:
			if message.Key().Text != "" {
				m.action.input += message.Key().Text
			}
		}
	case actionResultMode:
		switch key {
		case "esc", "enter":
			m.action.mode = actionClosed
		case "f":
			if m.action.overwriteRequest != nil {
				request := *m.action.overwriteRequest
				request.overwrite = true
				return m, m.startAction(request)
			}
		}
	}
	return m, nil
}

func (m *appModel) startRecommendation() tea.Cmd {
	if !m.currentEvidenceReady() {
		m.setActionError(fmt.Errorf("recommendations require ready, current evidence; collector state is %s", m.currentEvidenceState()))
		return nil
	}
	ref, ok := m.currentActionRef()
	if !ok {
		m.setActionError(fmt.Errorf("select a Pod, container, or workload first"))
		return nil
	}
	return m.startAction(actionRequest{kind: actionRecommend, ref: ref, pods: append([]api.PodSnapshot(nil), m.data.Pods...)})
}

func (m *appModel) startCompare() tea.Cmd {
	if !m.currentEvidenceReady() {
		m.setActionError(fmt.Errorf("comparison requires ready, current evidence; collector state is %s", m.currentEvidenceState()))
		return nil
	}
	pod, ok := m.currentActionPod()
	if !ok {
		m.setActionError(fmt.Errorf("live comparison requires a selected Pod or container"))
		return nil
	}
	if m.action.compareSource == nil {
		copy := pod
		m.action.compareSource = &copy
		m.action.mode = actionResultMode
		m.action.result = actionResult{title: "Comparison source marked", lines: []string{
			"First Pod: " + pod.Namespace + "/" + pod.PodName,
			"Close this message, select another Pod, then press x again.",
		}}
		m.action.err = nil
		return nil
	}
	before := *m.action.compareSource
	after := pod
	m.action.compareSource = nil
	return m.startAction(actionRequest{kind: actionCompare, before: &before, after: &after})
}

func (m *appModel) startCapture(overwrite bool) tea.Cmd {
	if m.statusErr != nil {
		m.setActionError(fmt.Errorf("capture is unavailable while the collector cannot be reached"))
		return nil
	}
	ref, ok := m.currentActionRef()
	if !ok || (ref.kind != entityPod && ref.kind != entityContainer) {
		m.setActionError(fmt.Errorf("capture requires a selected Pod or container"))
		return nil
	}
	request := actionRequest{
		kind:       actionCapture,
		ref:        ref,
		pods:       append([]api.PodSnapshot(nil), m.data.Pods...),
		nodes:      append([]api.NodeSnapshotStatus(nil), m.data.Nodes...),
		histories:  append([]api.PodHistory(nil), m.selectedHistory.series...),
		outputPath: strings.TrimSpace(m.action.input),
		overwrite:  overwrite,
		partial:    !m.opts.AllNamespaces || m.data.Reliability.State != api.CollectorReady,
	}
	reliability := m.data.Reliability
	request.reliability = &reliability
	request.caveats = m.captureCaveats()
	return m.startAction(request)
}

func (m *appModel) currentEvidenceReady() bool {
	return m.statusErr == nil && m.currentEvidenceState() == api.CollectorReady
}

func (m *appModel) currentEvidenceState() api.CollectorState {
	if m.statusErr != nil {
		return api.CollectorUnavailable
	}
	if m.data.Reliability.State == "" {
		return api.CollectorRebuilding
	}
	return m.data.Reliability.State
}

func (m *appModel) captureCaveats() []string {
	var caveats []string
	if !m.opts.AllNamespaces {
		caveats = append(caveats, "Cluster node summaries are omitted from a namespace-scoped capture.")
	}
	if m.data.Reliability.State != api.CollectorReady {
		caveats = append(caveats, "Collector evidence state at capture: "+string(m.data.Reliability.State)+".")
	}
	return caveats
}

func (m *appModel) startAction(request actionRequest) tea.Cmd {
	if m.actionExecutor == nil {
		m.actionExecutor = localActionExecutor{}
	}
	m.action.nextID++
	id := m.action.nextID
	m.action.activeID = id
	m.action.inFlight = true
	m.action.mode = actionResultMode
	m.action.err = nil
	m.action.result = actionResult{title: "Working…"}
	executor := m.actionExecutor
	ctx := m.ctx
	return func() tea.Msg {
		result, err := executor.Run(ctx, request)
		return actionMsg{id: id, result: result, err: err}
	}
}

func (m *appModel) completeAction(message actionMsg) {
	if message.id != m.action.activeID {
		return
	}
	m.action.inFlight = false
	m.action.result = message.result
	m.action.err = message.err
	m.action.overwriteRequest = nil
	if message.result.overwriteRequired {
		ref, ok := m.currentActionRef()
		if ok {
			m.action.overwriteRequest = &actionRequest{
				kind: actionCapture, ref: ref,
				pods: append([]api.PodSnapshot(nil), m.data.Pods...), nodes: append([]api.NodeSnapshotStatus(nil), m.data.Nodes...),
				histories: append([]api.PodHistory(nil), m.selectedHistory.series...), outputPath: message.result.outputPath,
				partial:     !m.opts.AllNamespaces || m.data.Reliability.State != api.CollectorReady,
				caveats:     m.captureCaveats(),
				reliability: &m.data.Reliability,
			}
		}
	}
}

func (m *appModel) setActionError(err error) {
	m.action.mode = actionResultMode
	m.action.err = err
	m.action.result = actionResult{title: "Action unavailable"}
}

func (m appModel) copyCurrentCommand() (tea.Model, tea.Cmd) {
	command, ok := m.currentCommand()
	if !ok {
		m.setActionError(fmt.Errorf("no safe command is available for the selected entity"))
		return m, nil
	}
	m.action.mode = actionResultMode
	m.action.err = nil
	m.action.result = actionResult{title: "Command copied", lines: []string{command, "Clipboard transport: OSC 52"}}
	return m, tea.Printf("%s", osc52Sequence(command))
}

func (m appModel) currentActionRef() (entityRef, bool) {
	if m.view == viewDetail && m.detail.kind != entityNone {
		return m.detail, true
	}
	return m.currentEntityRef()
}

func (m appModel) currentActionPod() (api.PodSnapshot, bool) {
	ref, ok := m.currentActionRef()
	if !ok {
		return api.PodSnapshot{}, false
	}
	return podForRef(ref, m.data.Pods)
}

func (m appModel) currentCommand() (string, bool) {
	ref, ok := m.currentActionRef()
	if !ok {
		return "", false
	}
	switch ref.kind {
	case entityPod, entityContainer:
		return "kubectl memlens explain pod " + ref.podName + " -n " + ref.namespace, true
	case entityWorkload:
		return "kubectl memlens explain workload " + ref.workloadKind + "/" + ref.name + " -n " + ref.namespace, true
	default:
		return "", false
	}
}

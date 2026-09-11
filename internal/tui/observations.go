package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observationview"
)

func (m appModel) restricted() bool {
	return m.opts.EvidencePlan != nil && m.opts.EvidencePlan.Mode == capability.Restricted
}

func (m appModel) fetchObservationsCmd() tea.Cmd {
	return func() tea.Msg {
		batch, err := m.observationReader.Current(m.ctx)
		if err != nil {
			return fetchMsg{generation: m.fetchGeneration, err: err}
		}
		return fetchMsg{generation: m.fetchGeneration, data: snapshotData{Observations: &batch, ObservationRows: observationview.Rows(batch), FetchedAt: batch.ReceivedAt, ContainersLoaded: true}}
	}
}

func (m appModel) hasData() bool {
	if m.restricted() {
		return len(m.data.ObservationRows) > 0
	}
	if m.view == viewNodes || m.view == viewDetail && m.detail.kind == entityNode {
		return len(m.data.Nodes) > 0 || len(m.data.Pods) > 0 || m.selectedNode.evidence != nil || m.selectedNode.history != nil
	}
	return len(m.data.Namespaces) > 0
}

func scopeForView(view viewMode) capability.Scope {
	switch view {
	case viewNodes:
		return capability.NodeScope
	case viewNamespaces:
		return capability.NamespaceScope
	case viewWorkloads:
		return capability.WorkloadScope
	case viewPods:
		return capability.PodScope
	case viewContainers:
		return capability.ContainerScope
	default:
		return ""
	}
}

func (m appModel) visibleObservationRows() []observationview.Row {
	scope := scopeForView(m.view)
	namespace, all := m.activeNamespace()
	spec := parseFilter(m.query)
	result := []observationview.Row{}
	if spec.severity != "" || spec.diagnosis != "" || spec.pressure != "" {
		return result
	}
	for _, row := range m.data.ObservationRows {
		if row.Scope != scope || (scope != capability.NodeScope && !all && row.Namespace != namespace) {
			continue
		}
		if (scope == capability.PodScope || scope == capability.ContainerScope) && !m.observationInDrill(row) {
			continue
		}
		if !row.Matches(spec.text) || !observationMatchesState(row, spec, time.Now()) {
			continue
		}
		if spec.owner != "" && !strings.Contains(strings.ToLower(row.WorkloadKind+"/"+row.WorkloadName), spec.owner) {
			continue
		}
		result = append(result, row)
	}
	order := observationview.ByMemory
	if m.sort == sortName {
		order = observationview.ByName
	}
	observationview.Sort(result, order)
	return result
}

func (m appModel) observationInDrill(row observationview.Row) bool {
	if m.currentNode != "" && row.NodeName != m.currentNode {
		return false
	}
	return m.currentWorkloadName == "" || row.WorkloadName == m.currentWorkloadName && row.WorkloadKind == m.currentWorkloadKind
}

func observationMatchesState(row observationview.Row, spec filterSpec, now time.Time) bool {
	state := observationview.State(row, now)
	switch spec.state {
	case "":
		return true
	case "complete":
		return state == "fresh"
	case "incomplete":
		return row.WorkingSet == nil || row.WorkingSet.Evidence.Completeness != capability.Complete
	default:
		return state == spec.state
	}
}

func observationRef(row observationview.Row) entityRef {
	ref := entityRef{namespace: row.Namespace, name: row.Name, podName: row.PodName, nodeName: row.NodeName, workloadKind: row.WorkloadKind}
	switch row.Scope {
	case capability.NodeScope:
		ref.kind = entityNode
	case capability.NamespaceScope:
		ref.kind = entityNamespace
	case capability.WorkloadScope:
		ref.kind, ref.workloadKind = entityWorkload, row.Kind
	case capability.PodScope:
		ref.kind = entityPod
	case capability.ContainerScope:
		ref.kind, ref.containerName = entityContainer, row.Name
	}
	return ref
}

func (m appModel) observationForRef(ref entityRef) (observationview.Row, bool) {
	for _, row := range m.data.ObservationRows {
		if row.Key() == entityKey(ref) {
			return row, true
		}
	}
	return observationview.Row{}, false
}

func (m appModel) observationDetail(ref entityRef, width int) []string {
	row, ok := m.observationForRef(ref)
	if !ok {
		return []string{"The selected object is no longer present in the authorised current observations."}
	}
	return wrapText(observationview.Detail(row, time.Now()), width)
}

func (m appModel) sortLabel() string {
	if m.nodeTarget() != "" && m.selectedNode.rank != "" {
		return "contributors: " + string(m.selectedNode.rank)
	}
	if m.restricted() && m.sort != sortName {
		return "working set desc"
	}
	return m.sort.String()
}

func (m *appModel) cycleSort() {
	if !m.restricted() {
		m.sort = nextSort(m.sort)
		return
	}
	if m.sort == sortName {
		m.sort = sortTotal
		return
	}
	m.sort = sortName
}

func (m appModel) evidenceStateLabel() string {
	if !m.restricted() {
		return string(m.currentEvidenceState())
	}
	if m.data.Observations == nil {
		return "unreported"
	}
	for _, row := range m.data.ObservationRows {
		if observationview.State(row, time.Now()) == "stale" {
			return "stale"
		}
	}
	return string(m.data.Observations.Completeness)
}

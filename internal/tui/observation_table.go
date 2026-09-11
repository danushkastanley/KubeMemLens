package tui

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observationview"
)

// One table projection serves all restricted entities inside the existing
// layout, selection, viewport and split-detail frame.
func (m appModel) renderObservationTable(width int) string {
	spec := parseFilter(m.query)
	if spec.severity != "" || spec.diagnosis != "" || spec.pressure != "" {
		return truncateLines([]string{"Severity, diagnosis and pressure filters require deep evidence.", "Use text, owner or state filters."}, width)
	}
	rows := m.visibleObservationRows()
	if len(rows) == 0 {
		return "No authorised observations match the current filter."
	}
	wide := width >= 70
	headers := []string{"NAME", "WORKING SET", "STATE"}
	widths := []int{width - 28, 13, 10}
	numeric := numericIndexes(1)
	if wide {
		headers = []string{"NAMESPACE", "NAME", "WORKING SET", "AGE", "STATE"}
		widths = []int{16, width - 58, 13, 10, 10}
		numeric = numericIndexes(2)
	}
	lines := []string{" " + tableRow(headers, widths, nil)}
	viewport := m.currentViewport()
	start, end := viewport.visibleRange()
	now := time.Now()
	for i := start; i < end && i < len(rows); i++ {
		row := rows[i]
		name := row.Name
		if row.Scope == capability.ContainerScope {
			name = row.PodName + "/" + row.Name
		}
		if row.Scope == capability.WorkloadScope {
			name = row.Kind + "/" + row.Name
		}
		if !wide && row.Namespace != "" && m.opts.AllNamespaces {
			name = row.Namespace + "/" + name
		}
		cells := []string{name, observationview.Memory(row), observationview.State(row, now)}
		if wide {
			cells = []string{row.Namespace, name, observationview.Memory(row), observationview.Age(observationview.Evidence(row).CapturedAt, now), observationview.State(row, now)}
		}
		prefix := " "
		if i == viewport.selected {
			prefix = ">"
		}
		lines = append(lines, prefix+tableRow(cells, widths, numeric))
	}
	return truncateLines(lines, width)
}

func (m appModel) restrictedHelpLines() []string {
	return []string{observationview.RestrictedHelp,
		"Deep mode depends on cluster policy. It needs an authorised collector and node cgroup access.",
		"s cycles working set and name; owner: and state: filters use Kubernetes evidence.",
		observationview.DeepUnavailable}
}

func (m appModel) restrictedEmpty(width int) string {
	lines := []string{"No authorised Pod or Node observations were returned.", "Metrics may be absent; Pod resource and status rows remain visible when available.", "Press r to refresh or use status -n <namespace> to inspect source availability."}
	return truncateLines(lines, width)
}

func (m appModel) observationInlineDetail(width int) []string {
	ref, ok := m.currentEntityRef()
	if !ok {
		return []string{"No selected entity."}
	}
	return m.observationDetail(ref, width)
}

func (m appModel) sourceLabel() string {
	label := m.evidenceLabel()
	if !m.restricted() {
		return label
	}
	var oldest time.Time
	for _, row := range m.data.ObservationRows {
		at := observationview.Evidence(row).CapturedAt
		if !at.IsZero() && (oldest.IsZero() || at.Before(oldest)) {
			oldest = at
		}
	}
	age := "no sample"
	if !oldest.IsZero() {
		age = FormatAge(oldest) + " old"
		if oldest.After(time.Now()) {
			age = "future"
		}
	}
	return label + " / " + age + " / " + m.view.String()
}

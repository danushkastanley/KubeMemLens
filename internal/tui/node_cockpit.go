package tui

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
)

func (m appModel) nodeCockpitLines(name string, width int) []string {
	s := m.selectedNode
	if s.name != name {
		return nodeview.Wrap(append([]string{"Node context not loaded; observed-charge fallback:"}, m.fallbackNodeDetailLines(name)...), width)
	}
	lines := []string{"Node cockpit: " + name, "s rank contributors; Enter drill into Pods; C capture; Space pause"}
	if m.paused {
		lines = append(lines, "Paused: Node polling stopped.")
	}
	if s.inFlight {
		lines = append(lines, "Refreshing Node evidence and history...")
	}
	if s.err != nil {
		lines = append(lines, "Node context unavailable: "+s.err.Error())
	}
	if s.evidence != nil {
		if s.err != nil {
			lines = append(lines, "Retained Node-only facts from "+s.updatedAt.UTC().Format(time.RFC3339)+"; cached contributors removed.")
		}
		lines = append(lines, nodeview.Lines(*s.evidence, time.Now().UTC(), width)...)
	} else {
		lines = append(lines, "Optional Node context is unavailable; observed-charge fallback follows.")
		lines = append(lines, m.fallbackNodeDetailLines(name)...)
	}
	lines = append(lines, "", "Node trend / retained source samples:")
	if s.historyErr != nil {
		lines = append(lines, "History refresh failed: "+s.historyErr.Error())
	}
	if s.history != nil {
		if s.historyErr != nil {
			lines = append(lines, "Showing last-good Node-only history from "+s.historyUpdatedAt.UTC().Format(time.RFC3339))
		}
		lines = append(lines, nodeview.HistoryLines(*s.history, width)...)
	} else {
		lines = append(lines, "Node history: unavailable or rebuilding.")
	}
	lines = append(lines, "", "Next commands:", "kubectl memlens explain node "+name, "kubectl memlens history node "+name, "kubectl memlens capture --node "+name+" --include-history -o node-incident.json")
	return nodeview.Wrap(lines, width)
}

func (m appModel) nodeInlineLines(name string, width int) []string {
	s := m.selectedNode
	if s.name != name || s.evidence == nil {
		return m.nodeCockpitLines(name, width)
	}
	a := s.evidence.Analysis
	lines := []string{"Node: " + name, string(a.Severity) + "; confidence " + string(a.Confidence), "Source: " + string(a.Facts.Availability) + "; record " + string(nodeview.RecordFreshness(s.evidence.Record, time.Now().UTC()))}
	if o := s.evidence.Record.LastGood; o != nil {
		lines = append(lines, "Source age: "+FormatAge(o.Evidence.CapturedAt)+"; "+string(o.Evidence.Completeness))
	}
	if m.paused {
		lines = append(lines, "Paused: polling stopped")
	}
	if s.err != nil {
		lines = append(lines, "Retained Node-only facts; refresh failed")
	}
	if memory := a.Facts.Memory; memory != nil {
		lines = append(lines, "Node usage: "+tuiOptionalNodeBytes(memory.UsageBytes), "Available: "+tuiOptionalNodeBytes(memory.AvailableBytes), "Working set: "+tuiOptionalNodeBytes(memory.WorkingSetBytes), "RSS: "+tuiOptionalNodeBytes(memory.RSSBytes))
	}
	lines = append(lines, "Observed Pod charge: "+tuiOptionalNodeBytes(a.ObservedPodCharge), "Unaccounted estimate: "+tuiOptionalNodeBytes(a.Unaccounted.Bytes), "Values overlap; do not stack.", "Contributors ("+string(s.rank)+"):")
	if r := a.Rankings; r != nil {
		for _, row := range r.Pods {
			lines = append(lines, fmt.Sprintf("%s/%s: %s", row.Namespace, row.Name, tuiOptionalNodeBytes(&row.Charge.Total)))
		}
	} else {
		lines = append(lines, "Contributor access: "+string(a.ContributorAccess))
	}
	lines = append(lines, "e all Node fields and history; Tab scroll", "s rank; Enter Pods; C capture")
	return nodeview.Wrap(lines, width)
}

func tuiOptionalNodeBytes(value *uint64) string {
	if value == nil {
		return "unreported"
	}
	return tuiKnownBytes(*value, true)
}

func (m appModel) nodeAnalysisFor(name string) *nodeanalysis.Analysis {
	if m.selectedNode.name == name && m.selectedNode.evidence != nil {
		return &m.selectedNode.evidence.Analysis
	}
	return nil
}

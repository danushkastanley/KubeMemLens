package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/explain"
)

// filterKey names a structured filter whose values form a closed set.
type filterKey int

const (
	filterSeverity filterKey = iota
	filterDiagnosis
	filterPressure
	filterState
)

var (
	pressureValues = []string{"true", "yes", "1", "false", "no", "0"}
	stateValues    = []string{"stale", "fresh", "incomplete", "complete"}
)

type unknownFilter struct {
	key   filterKey
	token string
}

func (key filterKey) accepts(value string) bool {
	switch key {
	case filterSeverity:
		return slices.ContainsFunc(explain.Severities(), func(s explain.Severity) bool { return strings.EqualFold(string(s), value) })
	case filterDiagnosis:
		// Diagnosis filters match by substring, so any part of a name is known.
		return slices.ContainsFunc(explain.Diagnoses(), func(d explain.Diagnosis) bool { return strings.Contains(string(d), value) })
	case filterPressure:
		return slices.Contains(pressureValues, value)
	default:
		return slices.Contains(stateValues, value)
	}
}

// guidance is the sentence that tells the reader what the key accepts.
func (key filterKey) guidance() string {
	switch key {
	case filterSeverity:
		values := make([]string, 0, 4)
		for _, severity := range explain.Severities() {
			values = append(values, string(severity))
		}
		return "Use " + spokenList(values) + "."
	case filterDiagnosis:
		values := make([]string, 0, 10)
		for _, diagnosis := range explain.Diagnoses() {
			values = append(values, string(diagnosis))
		}
		return "Use part of " + spokenList(values) + "."
	case filterPressure:
		return "Use true or false."
	default:
		return "Use " + spokenList(stateValues) + "."
	}
}

// spokenList joins two or more values as they would be read aloud: "a, b or c".
func spokenList(values []string) string {
	return strings.Join(values[:len(values)-1], ", ") + " or " + values[len(values)-1]
}

// filterNotice names the first unknown filter token the current view would
// otherwise apply, or returns "" when there is none or no table is on screen.
func (m appModel) filterNotice() string {
	if m.help || m.historyPanel.open || m.replicaPanel.open || m.sessionPanel.open {
		return ""
	}
	keys := m.interpretedFilterKeys()
	spec := parseFilter(m.query)
	unknown := make([]unknownFilter, 0)
	for _, candidate := range spec.unknown {
		if slices.Contains(keys, candidate.key) {
			unknown = append(unknown, candidate)
		}
	}
	// parseFilter keeps any state value because restricted views match it
	// against their own labels; only the Pod table limits it to a closed set.
	if slices.Contains(keys, filterState) && spec.state != "" && !filterState.accepts(spec.state) {
		unknown = append(unknown, unknownFilter{key: filterState, token: "state:" + spec.state})
	}
	if len(unknown) == 0 {
		return ""
	}
	notice := "Unknown filter " + unknown[0].token + ". " + unknown[0].key.guidance()
	switch more := len(unknown) - 1; more {
	case 0:
		return notice
	case 1:
		return notice + " 1 more filter is unknown."
	default:
		return notice + fmt.Sprintf(" %d more filters are unknown.", more)
	}
}

// interpretedFilterKeys lists the closed-set keys the current table applies.
// Restricted views have none: they answer severity, diagnosis and pressure
// with their own deep-evidence message and match state against row labels.
func (m appModel) interpretedFilterKeys() []filterKey {
	if m.restricted() {
		return nil
	}
	switch m.view {
	case viewPods:
		return []filterKey{filterSeverity, filterDiagnosis, filterPressure, filterState}
	case viewWorkloads, viewContainers:
		return []filterKey{filterSeverity, filterDiagnosis, filterPressure}
	default:
		return nil
	}
}

// withFilterNotice puts the notice in place of the key hints. While the query
// is typed, the search hints follow the notice, so a narrow terminal truncates
// the hints rather than the notice.
func (m appModel) withFilterNotice(footer string) string {
	notice := m.filterNotice()
	if notice == "" {
		return footer
	}
	if m.searching {
		return notice + " · " + footer
	}
	return notice
}

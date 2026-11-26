package tui

import (
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type filterSpec struct {
	text      string
	severity  string
	diagnosis string
	pressure  string
	owner     string
	state     string
	// unknown lists, in query order, severity, diagnosis and pressure tokens
	// whose value is not accepted. They are not applied, so they neither empty
	// nor invert a table.
	unknown []unknownFilter
}

func parseFilter(query string) filterSpec {
	var spec filterSpec
	text := make([]string, 0)
	for _, token := range strings.Fields(strings.ToLower(strings.TrimSpace(query))) {
		key, value, found := strings.Cut(token, ":")
		if !found || value == "" {
			text = append(text, token)
			continue
		}
		switch key {
		case "severity", "sev":
			spec.severity = spec.accept(filterSeverity, token, value, spec.severity)
		case "diagnosis", "diag":
			spec.diagnosis = spec.accept(filterDiagnosis, token, value, spec.diagnosis)
		case "pressure":
			spec.pressure = spec.accept(filterPressure, token, value, spec.pressure)
		case "owner", "workload":
			spec.owner = value
		case "state":
			// Kept as typed: restricted views match it against any state label
			// a row shows, and the Pod table ignores a value it does not know.
			spec.state = value
		default:
			text = append(text, token)
		}
	}
	spec.text = strings.Join(text, " ")
	return spec
}

// accept returns value when key accepts it. Otherwise it records token as
// unknown and keeps the value already in force.
func (spec *filterSpec) accept(key filterKey, token, value, current string) string {
	if key.accepts(value) {
		return value
	}
	spec.unknown = append(spec.unknown, unknownFilter{key: key, token: token})
	return current
}

// requestsDeepEvidence reports a severity, diagnosis or pressure token, known
// or not. Restricted views cannot evaluate any of them.
func (spec filterSpec) requestsDeepEvidence() bool {
	return spec.severity != "" || spec.diagnosis != "" || spec.pressure != "" || len(spec.unknown) > 0
}

func (spec filterSpec) matchesPod(pod api.PodSnapshot, now time.Time, staleAfter time.Duration) bool {
	result := explain.AnalyzePodAt(pod, now)
	if spec.text != "" && !podMatches(pod, result.Diagnosis, spec.text) {
		return false
	}
	if !matchesMemoryFilters(spec, pod.Memory, string(result.Severity), string(result.Diagnosis)) {
		return false
	}
	if spec.owner != "" && !containsAny([]string{pod.Context.OwnerKind, pod.Context.OwnerName, pod.Context.WorkloadKind, pod.Context.WorkloadName}, spec.owner) {
		return false
	}
	stale := podEvidenceStale(pod, now, staleAfter)
	switch spec.state {
	case "stale":
		return stale
	case "fresh":
		return !stale
	case "incomplete":
		return pod.Completeness == api.EvidencePartial || !result.EvidenceWindow.DeltaKnown || !result.EvidenceWindow.DeltaComplete
	case "complete":
		return pod.Completeness != api.EvidencePartial && result.EvidenceWindow.DeltaKnown && result.EvidenceWindow.DeltaComplete
	default:
		return true
	}
}

func matchesMemoryFilters(spec filterSpec, memory model.MemoryBreakdown, severity, diagnosis string) bool {
	if spec.severity != "" && !strings.EqualFold(severity, spec.severity) {
		return false
	}
	if spec.diagnosis != "" && !strings.Contains(strings.ToLower(diagnosis), spec.diagnosis) {
		return false
	}
	if spec.pressure != "" {
		want := spec.pressure == "true" || spec.pressure == "yes" || spec.pressure == "1"
		if memory.HasPressureRisk() != want {
			return false
		}
	}
	return true
}

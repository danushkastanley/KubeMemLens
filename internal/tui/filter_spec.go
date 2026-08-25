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
			spec.severity = value
		case "diagnosis", "diag":
			spec.diagnosis = value
		case "pressure":
			spec.pressure = value
		case "owner", "workload":
			spec.owner = value
		case "state":
			spec.state = value
		default:
			text = append(text, token)
		}
	}
	spec.text = strings.Join(text, " ")
	return spec
}

func (spec filterSpec) matchesPod(pod api.PodSnapshot, now time.Time, staleAfter time.Duration) bool {
	result := explain.AnalyzePod(pod)
	if spec.text != "" && !podMatches(pod, spec.text) {
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

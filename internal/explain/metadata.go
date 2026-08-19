package explain

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// Severity describes investigation urgency, not certainty or business impact.
// Confidence remains an independent statement about evidence strength.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// EvidenceWindow keeps point-in-time observations separate from counter
// deltas. Workload roll-ups can span more than one node sampling window.
type EvidenceWindow struct {
	ObservationStart time.Time
	ObservationEnd   time.Time
	DeltaStart       time.Time
	DeltaEnd         time.Time
	DeltaKnown       bool
	DeltaComplete    bool
	DeltaUniform     bool
	DeltaWindowCount int
}

func AnalyzeContainer(container api.ContainerSnapshot) Result {
	return attachEvidenceWindow(Analyze(container.Memory), evidenceWindow([]api.ContainerSnapshot{container}, nil))
}

func AnalyzeWorkload(workload api.WorkloadSnapshot) Result {
	containers := make([]api.ContainerSnapshot, 0)
	fallbacks := make([]time.Time, 0, len(workload.Pods)+1)
	for _, pod := range workload.Pods {
		containers = append(containers, pod.Containers...)
		fallbacks = append(fallbacks, pod.CapturedAt)
	}
	fallbacks = append(fallbacks, workload.CapturedAt)
	return attachEvidenceWindow(Analyze(workload.Memory), evidenceWindow(containers, fallbacks))
}

func attachPodEvidenceWindow(result Result, pod api.PodSnapshot) Result {
	return attachEvidenceWindow(result, evidenceWindow(pod.Containers, []time.Time{pod.CapturedAt}))
}

func attachEvidenceWindow(result Result, window EvidenceWindow) Result {
	result.EvidenceWindow = window
	switch {
	case window.ObservationStart.IsZero():
		result.Caveats = appendUnique(result.Caveats, "The capture timestamp is unavailable, so this diagnosis cannot establish when the gauge values were observed.")
	case !window.DeltaKnown:
		result.Caveats = appendUnique(result.Caveats, "Counter deltas have no elapsed window for this first observation or after a container identity change.")
	case !window.DeltaComplete:
		result.Caveats = appendUnique(result.Caveats, "The counter window is incomplete because at least one included container lacks a prior observation.")
	case !window.DeltaUniform:
		result.Caveats = appendUnique(result.Caveats, "The aggregate spans multiple exact counter windows; use per-replica evidence before correlating a delta to one moment.")
	}
	return result
}

func finaliseResult(result Result, memory model.MemoryBreakdown) Result {
	result.Severity = severityFor(result.Diagnosis)
	result.Caveats = diagnosisCaveats(result.Diagnosis, memory)
	result.Caveats = appendUnique(result.Caveats, "Cgroup composition shows charged memory ownership; it does not identify individual files, allocations, or processes.")
	return result
}

func severityFor(diagnosis Diagnosis) Severity {
	switch diagnosis {
	case DiagnosisOOMRisk:
		return SeverityCritical
	case DiagnosisPressure:
		return SeverityHigh
	case DiagnosisLimitRisk:
		return SeverityMedium
	default:
		return SeverityInfo
	}
}

func diagnosisCaveats(diagnosis Diagnosis, memory model.MemoryBreakdown) []string {
	switch diagnosis {
	case DiagnosisOOMRisk:
		if memory.EventDeltasKnown || memory.LocalEventDeltasKnown {
			return []string{"A cgroup event delta proves that a counter changed during the sampled window, but not which allocation triggered it."}
		}
		return []string{"Cumulative cgroup events may predate this observation; Kubernetes termination state can also outlive the original cgroup."}
	case DiagnosisPressure:
		return []string{"Pressure signals show reclaim impact or node contention, but do not identify which workload or allocation caused it."}
	case DiagnosisLimitRisk:
		return []string{"Limit headroom is a point-in-time value; a future burst or reclaim response cannot be predicted from one sample."}
	case DiagnosisNormal:
		return []string{"No dominant signal in one snapshot does not prove that earlier or intermittent pressure was absent."}
	default:
		return []string{"The composition is measured directly, while the likely explanation is a single-snapshot heuristic that requires workload-specific confirmation."}
	}
}

func evidenceWindow(containers []api.ContainerSnapshot, fallbackObservations []time.Time) EvidenceWindow {
	window := EvidenceWindow{DeltaComplete: len(containers) > 0, DeltaUniform: true}
	type deltaKey struct {
		start int64
		end   int64
	}
	deltaWindows := map[deltaKey]struct{}{}
	knownDeltas := 0
	for _, container := range containers {
		includeObservation(&window, container.CapturedAt)
		if !container.DeltaWindowKnown || container.DeltaStartedAt.IsZero() || container.CapturedAt.IsZero() {
			window.DeltaComplete = false
			continue
		}
		knownDeltas++
		includeDelta(&window, container.DeltaStartedAt, container.CapturedAt)
		deltaWindows[deltaKey{start: container.DeltaStartedAt.UnixNano(), end: container.CapturedAt.UnixNano()}] = struct{}{}
	}
	for _, capturedAt := range fallbackObservations {
		includeObservation(&window, capturedAt)
	}
	window.DeltaKnown = knownDeltas > 0
	window.DeltaWindowCount = len(deltaWindows)
	window.DeltaUniform = window.DeltaKnown && len(deltaWindows) == 1
	return window
}

func includeObservation(window *EvidenceWindow, capturedAt time.Time) {
	if capturedAt.IsZero() {
		return
	}
	if window.ObservationStart.IsZero() || capturedAt.Before(window.ObservationStart) {
		window.ObservationStart = capturedAt
	}
	if window.ObservationEnd.IsZero() || capturedAt.After(window.ObservationEnd) {
		window.ObservationEnd = capturedAt
	}
}

func includeDelta(window *EvidenceWindow, start, end time.Time) {
	if window.DeltaStart.IsZero() || start.Before(window.DeltaStart) {
		window.DeltaStart = start
	}
	if window.DeltaEnd.IsZero() || end.After(window.DeltaEnd) {
		window.DeltaEnd = end
	}
}

func (window EvidenceWindow) ObservationDescription() string {
	if window.ObservationStart.IsZero() {
		return "unavailable"
	}
	if window.ObservationStart.Equal(window.ObservationEnd) {
		return window.ObservationStart.UTC().Format(time.RFC3339Nano) + " (instantaneous gauge sample)"
	}
	return fmt.Sprintf("%s to %s (cross-snapshot gauge range)", formatTimestamp(window.ObservationStart), formatTimestamp(window.ObservationEnd))
}

func (window EvidenceWindow) DeltaDescription() string {
	if !window.DeltaKnown {
		return "unavailable"
	}
	quality := "complete"
	if !window.DeltaComplete {
		quality = "incomplete"
	}
	shape := "uniform"
	if !window.DeltaUniform {
		shape = fmt.Sprintf("%d distinct windows", window.DeltaWindowCount)
	}
	return fmt.Sprintf("%s to %s (%s; %s)", formatTimestamp(window.DeltaStart), formatTimestamp(window.DeltaEnd), quality, shape)
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func appendUnique(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if _, exists := seen[value]; exists {
			continue
		}
		values = append(values, value)
		seen[value] = struct{}{}
	}
	return values
}

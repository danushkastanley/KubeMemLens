package tui

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/explain"
)

// withPodCgroupEventRisk scores recent Pod cgroup events with the weights
// memoryRisk gives the containers' own events. They are counted in the Pod's
// memory.events.local, which no container reports, so nothing is counted twice.
func withPodCgroupEventRisk(risk riskPresentation, events explain.PodCgroupEvents) riskPresentation {
	if !events.Recorded() {
		return risk
	}
	if len(risk.reasons) == 1 && risk.reasons[0] == severityOnlyReason {
		risk.reasons = nil
	}
	if events.OOMKill > 0 {
		risk.score += 40
		risk.reasons = append(risk.reasons, fmt.Sprintf("%d recent Pod cgroup OOM kill event(s)", events.OOMKill))
	} else if events.OOM > 0 {
		risk.score += 30
		risk.reasons = append(risk.reasons, fmt.Sprintf("%d recent Pod cgroup OOM event(s)", events.OOM))
	}
	if events.Max > 0 {
		risk.score += 20
		risk.reasons = append(risk.reasons, fmt.Sprintf("%d recent Pod cgroup memory.max event(s)", events.Max))
	}
	return risk
}

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type riskPresentation struct {
	score   int
	label   string
	reasons []string
	stale   bool
}

func podRisk(pod api.PodSnapshot, now time.Time, staleAfter time.Duration) riskPresentation {
	result := explain.AnalyzePod(pod)
	risk := memoryRisk(pod.Memory, result.Severity)
	if pod.Context.NodeMemoryPressure == "True" {
		risk.score += 25
		risk.reasons = append(risk.reasons, "node MemoryPressure is true")
	}
	if pod.Context.LastTerminationKnown {
		risk.score += 10
		risk.reasons = append(risk.reasons, "a recent container termination is reported")
	}
	if isStale(pod.CapturedAt, now, staleAfter) {
		risk.stale = true
		risk.label = "STALE"
		risk.reasons = append(risk.reasons, "snapshot is stale")
	}
	if !result.EvidenceWindow.DeltaKnown {
		risk.reasons = append(risk.reasons, "counter delta window is unavailable")
	}
	return risk
}

func memoryRisk(memory model.MemoryBreakdown, severity explain.Severity) riskPresentation {
	risk := riskPresentation{label: severityLabel(severity)}
	switch severity {
	case explain.SeverityCritical:
		risk.score = 400
	case explain.SeverityHigh:
		risk.score = 300
	case explain.SeverityMedium:
		risk.score = 200
	default:
		risk.score = 100
	}
	oom, kill, high, maxEvents := memory.RecentEventCounts()
	if kill > 0 {
		risk.score += 40
		risk.reasons = append(risk.reasons, fmt.Sprintf("%d recent OOM kill event(s)", kill))
	} else if oom > 0 {
		risk.score += 30
		risk.reasons = append(risk.reasons, fmt.Sprintf("%d recent OOM event(s)", oom))
	}
	if maxEvents > 0 {
		risk.score += 20
		risk.reasons = append(risk.reasons, fmt.Sprintf("%d recent memory.max event(s)", maxEvents))
	}
	if high > 0 {
		risk.score += 10
		risk.reasons = append(risk.reasons, fmt.Sprintf("%d recent memory.high event(s)", high))
	}
	if memory.PressureKnown && memory.PSIFullAvg10 > 0 {
		risk.score += 15
		risk.reasons = append(risk.reasons, "full memory PSI is non-zero")
	}
	if memory.HasLimitRisk() {
		risk.score += 5
		risk.reasons = append(risk.reasons, "cgroup limit headroom is low")
	}
	if len(risk.reasons) == 0 {
		risk.reasons = []string{"ordering follows the existing diagnosis severity"}
	}
	return risk
}

func isStale(capturedAt, now time.Time, staleAfter time.Duration) bool {
	if capturedAt.IsZero() || now.IsZero() || staleAfter <= 0 {
		return false
	}
	return now.Sub(capturedAt) > staleAfter
}

func (m appModel) staleAfter() time.Duration {
	interval := m.opts.RefreshInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	threshold := interval * 3
	if threshold < 15*time.Second {
		threshold = 15 * time.Second
	}
	if threshold > time.Minute {
		threshold = time.Minute
	}
	return threshold
}

func (m appModel) riskNow() time.Time {
	if !m.lastRefresh.IsZero() {
		return m.lastRefresh
	}
	return time.Now()
}

func riskReasonsText(risk riskPresentation) string {
	return strings.Join(risk.reasons, "; ")
}

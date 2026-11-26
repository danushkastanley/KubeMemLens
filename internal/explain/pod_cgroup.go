package explain

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// PodCgroupEvents are the Pod cgroup's own memory.events.local increments in
// the sample window. Known is false when the collector computed no delta: the
// raw counters may then predate the first sample and are not recent evidence.
type PodCgroupEvents struct {
	Known   bool
	OOM     uint64
	OOMKill uint64
	Max     uint64
}

func RecentPodCgroupEvents(cgroup model.PodCgroupMemory) PodCgroupEvents {
	if !cgroup.LocalEventDeltasKnown {
		return PodCgroupEvents{}
	}
	return PodCgroupEvents{Known: true, OOM: cgroup.LocalOOMEventsDelta, OOMKill: cgroup.LocalOOMKillEventsDelta, Max: cgroup.LocalMaxEventsDelta}
}

// Recorded reports whether the window counted any event. A container's recent
// memory.max events are OOM evidence in Analyze, so the Pod's count here too.
func (e PodCgroupEvents) Recorded() bool {
	return e.OOM > 0 || e.OOMKill > 0 || e.Max > 0
}

// AnalyzePodMemory ranks a Pod's cgroup evidence in the order Analyze uses:
// OOM evidence, then pressure, then limit risk, then composition. The Pod
// cgroup adds OOM evidence from its own recent local events and, when its
// memory.max is the effective limit, stands in for the summed container
// limits. Without Pod cgroup evidence the result is Analyze(pod.Memory).
func AnalyzePodMemory(pod api.PodSnapshot) Result {
	limit := pod.EffectiveLimit()
	events := RecentPodCgroupEvents(pod.PodCgroup)
	if limit.Source != api.PodLimitPodCgroup {
		return withPodCgroupEvents(Analyze(pod.Memory), pod, limit, events)
	}
	return withPodCgroupEvents(analyzeUnderPodCgroupLimit(pod, limit), pod, limit, events)
}

func analyzeUnderPodCgroupLimit(pod api.PodSnapshot, limit api.PodLimit) Result {
	memory := pod.Memory
	switch {
	case memory.HasOOMRisk():
		// The summed container limit is not this Pod's limit, so oomRisk must
		// not describe usage against it; the Pod cgroup figure replaces it.
		unbounded := memory
		unbounded.MaxKnown, unbounded.MaxUnlimited, unbounded.MaxBytes = false, false, 0
		result := withConfidence(oomRisk(unbounded), memory)
		result.Signals = append(result.Signals, podCgroupLimitSignal(pod, limit))
		return result
	case memory.HasPressureRisk():
		return withConfidence(pressureRisk(memory), memory)
	case limit.AtRisk():
		return withConfidence(podCgroupLimitRisk(pod, limit), memory)
	default:
		// The Pod cgroup's memory.max is the Pod's budget for composition
		// thresholds such as when anonymous memory is material.
		budget := memory
		budget.MaxKnown, budget.MaxUnlimited, budget.MaxBytes = true, false, limit.LimitBytes
		return analyzeComposition(budget)
	}
}

// withPodCgroupEvents adds recent Pod cgroup events to an OOM result, or makes
// them the result: they outrank pressure, limit and composition evidence.
func withPodCgroupEvents(result Result, pod api.PodSnapshot, limit api.PodLimit, events PodCgroupEvents) Result {
	if !events.Recorded() {
		return result
	}
	if result.Diagnosis != DiagnosisOOMRisk {
		return podCgroupOOMRisk(pod, limit, events)
	}
	result.Signals = append(result.Signals, podCgroupEventSignals(events)...)
	if result.Confidence != ConfidenceHigh {
		result.Confidence, result.ConfidenceReason = ConfidenceHigh, podCgroupEventConfidence
	}
	return result
}

const podCgroupEventConfidence = "A recent Pod cgroup memory.events.local delta directly supports this diagnosis."

func podCgroupOOMRisk(pod api.PodSnapshot, limit api.PodLimit, events PodCgroupEvents) Result {
	signals := podCgroupEventSignals(events)
	if limit.Source == api.PodLimitPodCgroup {
		signals = append(signals, podCgroupLimitSignal(pod, limit))
	}
	result := finaliseResult(Result{
		Diagnosis: DiagnosisOOMRisk,
		Summary:   "The Pod cgroup recorded OOM or memory.max events at its own limit.",
		LikelyExplanation: "The Pod's own cgroup, the parent of its containers and sandbox, reported these events in the sample window. " +
			"A Pod-level limit applies to all of them together, so a container can be affected while it has headroom under its own limit.",
		SuggestedChecks: []string{
			"Check pod events and container restart reasons.",
			"Compare the Pod cgroup charge with its memory.max and each container's share of it.",
			"Review recent deploys, traffic shifts, and workload-specific allocation changes.",
		},
		Signals: signals,
	}, pod.Memory)
	result.Confidence, result.ConfidenceReason = ConfidenceHigh, podCgroupEventConfidence
	return result
}

func podCgroupEventSignals(events PodCgroupEvents) []string {
	var signals []string
	if events.OOM > 0 {
		signals = append(signals, fmt.Sprintf("Pod cgroup recorded %d OOM event(s) at its own limit in the sample window", events.OOM))
	}
	if events.OOMKill > 0 {
		signals = append(signals, fmt.Sprintf("Pod cgroup recorded %d OOM kill(s) at its own limit in the sample window", events.OOMKill))
	}
	if events.Max > 0 {
		signals = append(signals, fmt.Sprintf("Pod cgroup reached its own memory.max %d time(s) in the sample window", events.Max))
	}
	return signals
}

func podCgroupLimitRisk(pod api.PodSnapshot, limit api.PodLimit) Result {
	headroom := limit.LimitBytes - minUint64(limit.UsedBytes, limit.LimitBytes)
	return Result{
		Diagnosis: DiagnosisLimitRisk,
		Summary:   "Current memory is close to the Pod cgroup's hard limit.",
		LikelyExplanation: "No recent OOM or pressure event is visible, but the Pod's own memory.max leaves little headroom. " +
			"That limit applies to all containers and the sandbox together, so a transient allocation in any of them could cross it before the next sample.",
		SuggestedChecks: []string{
			"Compare each container's charge with the Pod-level limit and recent traffic peaks.",
			"Check container restarts and the last termination reason.",
			"Investigate the dominant memory bucket before changing the Pod-level limit.",
		},
		Signals: []string{
			podCgroupLimitSignal(pod, limit),
			fmt.Sprintf("remaining Pod cgroup headroom is %s", model.FormatCompactBytes(headroom)),
		},
	}
}

// podCgroupLimitSignal names what was measured against the Pod cgroup's limit:
// its own memory.current, or the summed container charge when that was not read.
func podCgroupLimitSignal(pod api.PodSnapshot, limit api.PodLimit) string {
	percent, size := limit.UsageRatio()*100, model.FormatCompactBytes(limit.LimitBytes)
	if !pod.PodCgroup.CurrentKnown {
		return fmt.Sprintf("summed container usage is %.0f%% of the Pod cgroup's %s memory.max", percent, size)
	}
	return fmt.Sprintf("Pod cgroup usage is %.0f%% of its %s memory.max", percent, size)
}

// podLimitSignal is the Pod-level figure beside a container limit finding. The
// container-sum wording is the one used before schema 8.
func podLimitSignal(pod api.PodSnapshot) (string, bool) {
	limit := pod.EffectiveLimit()
	if limit.Source == api.PodLimitPodCgroup {
		return podCgroupLimitSignal(pod, limit), true
	}
	if !limit.Known || limit.Unlimited {
		return "", false
	}
	return fmt.Sprintf("Pod usage is %.0f%% of the summed container limits", limit.UsageRatio()*100), true
}

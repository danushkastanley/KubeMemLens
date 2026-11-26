package tui

import (
	"fmt"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	memmodel "github.com/danushkastanley/kube-memlens/internal/model"
)

// podLimitView is the Pod's memory with its limit, and the usage measured
// against that limit, taken from api.PodSnapshot.EffectiveLimit. Limit labels,
// headroom and risk read it so that a Pod and a container are formatted by the
// same code. TotalBytes may be the Pod cgroup's memory.current, which includes
// the sandbox, so composition must keep reading pod.Memory.
func podLimitView(pod api.PodSnapshot) memmodel.MemoryBreakdown {
	view := pod.Memory
	limit := pod.EffectiveLimit()
	view.TotalBytes, view.MaxKnown, view.MaxUnlimited, view.MaxBytes = limit.UsedBytes, limit.Known, limit.Unlimited, limit.LimitBytes
	return view
}

// podLimitSource names the limit's source for detail panes, only when the Pod
// cgroup's memory.max was read; otherwise the pane reads as it did before.
func podLimitSource(pod api.PodSnapshot) string {
	source, named := explain.NamedPodLimitSource(pod)
	if !named {
		return ""
	}
	return " (" + explain.PodLimitSourceWords(source) + ")"
}

// presentPod is presentMemory for a Pod row or pane: composition from the
// summed containers, the limit from the effective limit, and severity and
// signal that include the Pod cgroup's own evidence.
func presentPod(pod api.PodSnapshot, barWidth int, ascii bool) memoryPresentation {
	presentation := presentMemory(pod.Memory, barWidth, ascii)
	presentation.limit = limitLabel(podLimitView(pod), 8, ascii)
	presentation.signal = podIncidentLabel(pod)
	presentation.severity = severityLabel(explain.AnalyzePodMemory(pod).Severity)
	return presentation
}

// podIncidentLabel puts recent Pod cgroup events before the container label.
func podIncidentLabel(pod api.PodSnapshot) string {
	label := incidentLabel(pod.Memory)
	events := explain.RecentPodCgroupEvents(pod.PodCgroup)
	if !events.Recorded() {
		return label
	}
	parts := make([]string, 0, 3)
	if events.OOM > 0 || events.OOMKill > 0 {
		parts = append(parts, fmt.Sprintf("Pod OOM %d/%d", events.OOM, events.OOMKill))
	}
	if events.Max > 0 {
		parts = append(parts, fmt.Sprintf("Pod max %d", events.Max))
	}
	if label != "clear" && label != "baseline" {
		parts = append(parts, label)
	}
	return strings.Join(parts, " ")
}

// podCgroupDetailLines are the Pod cgroup's raw values for the detail pane,
// beside the container event counters. Nothing is shown when nothing was read.
func podCgroupDetailLines(pod api.PodSnapshot) []string {
	cgroup := pod.PodCgroup
	if cgroup == (memmodel.PodCgroupMemory{}) {
		return nil
	}
	limit := tuiKnownBytes(cgroup.MaxBytes, cgroup.MaxKnown)
	if cgroup.MaxKnown && cgroup.MaxUnlimited {
		limit = "unlimited"
	}
	events := "unknown"
	if recent := explain.RecentPodCgroupEvents(cgroup); recent.Known {
		events = fmt.Sprintf("oom %d · oom_kill %d · max %d (recent)", recent.OOM, recent.OOMKill, recent.Max)
	}
	return []string{
		"Pod cgroup charge:    " + tuiKnownBytes(cgroup.CurrentBytes, cgroup.CurrentKnown),
		"Pod cgroup limit:     " + limit,
		"Pod cgroup events:    " + events,
	}
}

// oomGroupKillLine follows the OOM kill counter; an unreported key is left
// out rather than shown as zero.
func oomGroupKillLine(memory memmodel.MemoryBreakdown) []string {
	if !memory.OOMGroupKill.Known {
		return nil
	}
	return []string{fmt.Sprintf("Group kill events:    %d", memory.OOMGroupKill.Events)}
}

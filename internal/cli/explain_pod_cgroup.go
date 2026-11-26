package cli

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// podCgroupExplanationSchemaVersion adds podCgroup, memory.zswap and
// memory.recentEvents.oomGroupKill to explanation schema 3. A document that
// carries none of them is written as schema 3, byte for byte as before, so a
// reader of schema 3 keeps working for replays and older agents.
const podCgroupExplanationSchemaVersion = 6

// podCgroupEvidence is the Pod's own cgroup. Unknown values have known=false;
// recentEvents.known=false means no delta was computed, so no count is recent.
type podCgroupEvidence struct {
	EffectiveLimitSource api.PodLimitSource     `json:"effectiveLimitSource"`
	Current              boundaryEvidence       `json:"current"`
	Limit                boundaryEvidence       `json:"limit"`
	RecentEvents         podCgroupEventEvidence `json:"recentEvents"`
}

type podCgroupEventEvidence struct {
	Known   bool   `json:"known"`
	OOM     uint64 `json:"oom"`
	OOMKill uint64 `json:"oomKill"`
	Max     uint64 `json:"max"`
}

type zswapEvidence struct {
	PoolBytes    uint64 `json:"poolBytes"`
	SwappedBytes uint64 `json:"swappedBytes"`
}

func podCgroupOutput(pod api.PodSnapshot) *podCgroupEvidence {
	cgroup := pod.PodCgroup
	if cgroup == (model.PodCgroupMemory{}) {
		return nil
	}
	events := explain.RecentPodCgroupEvents(cgroup)
	return &podCgroupEvidence{
		EffectiveLimitSource: pod.EffectiveLimit().Source,
		Current:              boundaryEvidence{Known: cgroup.CurrentKnown, Bytes: cgroup.CurrentBytes},
		Limit:                boundaryEvidence{Known: cgroup.MaxKnown, Unlimited: cgroup.MaxUnlimited, Bytes: cgroup.MaxBytes},
		RecentEvents:         podCgroupEventEvidence{Known: events.Known, OOM: events.OOM, OOMKill: events.OOMKill, Max: events.Max},
	}
}

func zswapOutput(memory model.MemoryBreakdown) *zswapEvidence {
	if !memory.Zswap.Known {
		return nil
	}
	return &zswapEvidence{PoolBytes: memory.Zswap.PoolBytes, SwappedBytes: memory.Zswap.SwappedBytes}
}

func oomGroupKillOutput(memory model.MemoryBreakdown) *uint64 {
	count, known := explain.RecentOOMGroupKill(memory)
	if !known {
		return nil
	}
	return &count
}

func explanationSchemaVersion(document explanationDocument) int {
	carries := document.PodCgroup != nil || memoryCarriesSchema6(document.Memory)
	for _, container := range document.Containers {
		carries = carries || memoryCarriesSchema6(container.Memory)
	}
	for _, replica := range document.Replicas {
		carries = carries || replica.PodCgroup != nil || memoryCarriesSchema6(replica.Memory)
	}
	if carries {
		return podCgroupExplanationSchemaVersion
	}
	return api.CurrentExplanationSchemaVersion
}

func memoryCarriesSchema6(memory memoryEvidence) bool {
	return memory.Zswap != nil || memory.RecentEvents.OOMGroupKill != nil
}

// podCgroupTextLines are the Pod cgroup lines of `explain pod`. They are left
// out when nothing was read, so output from older agents and replays is
// unchanged.
func podCgroupTextLines(pod api.PodSnapshot) []string {
	cgroup := pod.PodCgroup
	if cgroup == (model.PodCgroupMemory{}) {
		return nil
	}
	var lines []string
	if source, named := explain.NamedPodLimitSource(pod); named {
		lines = append(lines, "Limit source:         "+explain.PodLimitSourceWords(source))
	}
	return append(lines,
		"Pod cgroup charge:    "+knownBytes(cgroup.CurrentBytes, cgroup.CurrentKnown),
		"Pod cgroup limit:     "+podCgroupLimitText(cgroup),
		"Pod cgroup events:    "+podCgroupEventsText(explain.RecentPodCgroupEvents(cgroup)),
	)
}

func knownBytes(bytes uint64, known bool) string {
	if !known {
		return "unknown"
	}
	return model.FormatBytes(bytes)
}

func podCgroupLimitText(cgroup model.PodCgroupMemory) string {
	if cgroup.MaxKnown && cgroup.MaxUnlimited {
		return "unlimited"
	}
	return knownBytes(cgroup.MaxBytes, cgroup.MaxKnown)
}

func podCgroupEventsText(events explain.PodCgroupEvents) string {
	if !events.Known {
		return "unknown"
	}
	return fmt.Sprintf("oom=%d oom_kill=%d max=%d in the sample window", events.OOM, events.OOMKill, events.Max)
}

// hardLimitText is the Hard limit line's value, or "" when no limit was read.
func hardLimitText(memory model.MemoryBreakdown) string {
	if !memory.MaxKnown {
		return ""
	}
	if memory.MaxUnlimited {
		return "unlimited"
	}
	return fmt.Sprintf("%s (%.0f%% used)", model.FormatBytes(memory.MaxBytes), memory.LimitUsageRatio()*100)
}

func zswapText(zswap model.Zswap) string {
	return fmt.Sprintf("%s pool holding %s swapped out", model.FormatBytes(zswap.PoolBytes), model.FormatBytes(zswap.SwappedBytes))
}

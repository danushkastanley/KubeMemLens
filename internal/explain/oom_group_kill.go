package explain

import "github.com/danushkastanley/kube-memlens/internal/model"

// RecentOOMGroupKill reads oom_group_kill the way RecentEventCounts reads
// oom_kill: the memory.events.local counter when local events are read,
// otherwise memory.events, as a delta when the collector computed one and as
// the raw counter otherwise. Known is false when the kernel did not report the
// key; the count is then unknown, not zero.
func RecentOOMGroupKill(m model.MemoryBreakdown) (count uint64, known bool) {
	count, _, known = recentOOMGroupKill(m)
	return count, known
}

// recentOOMGroupKill also names the file and whether the count is a delta, in
// the form oomRisk uses for its other event signals.
func recentOOMGroupKill(m model.MemoryBreakdown) (uint64, string, bool) {
	group := m.OOMGroupKill
	if m.LocalEventsKnown {
		if !group.LocalKnown {
			return 0, "", false
		}
		if group.LocalDeltasKnown {
			return group.LocalEventsDelta, "memory.events.local delta", true
		}
		return group.LocalEvents, "memory.events.local", true
	}
	if !group.Known {
		return 0, "", false
	}
	if group.DeltasKnown {
		return group.EventsDelta, "memory.events delta", true
	}
	return group.Events, "memory.events", true
}

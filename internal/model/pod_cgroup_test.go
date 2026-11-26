package model

import "testing"

// Whatever the agent sends and whatever the previous sample held, the
// collector-owned deltas are recomputed, never exceed their counters, are zero
// on a first sample and after a counter reset, and leave a valid value.
func FuzzEventDeltasForSchema8Counters(f *testing.F) {
	f.Add(uint64(5), uint64(3), uint64(9), true, true, true, uint64(7))
	f.Add(uint64(1), uint64(4), uint64(0), true, true, true, uint64(0))
	f.Add(uint64(2), uint64(0), uint64(2), true, false, false, uint64(1))
	f.Add(uint64(0), uint64(0), uint64(0), false, true, true, uint64(0))
	f.Fuzz(func(t *testing.T, current, previous, agentDelta uint64, currentKnown, previousKnown, hasPrevious bool, other uint64) {
		pod := PodCgroupMemory{LocalEventsKnown: currentKnown, LocalOOMEventsDelta: agentDelta, LocalOOMKillEventsDelta: agentDelta, LocalMaxEventsDelta: agentDelta}
		last := PodCgroupMemory{LocalEventsKnown: previousKnown}
		group := OOMGroupKill{Known: currentKnown, LocalKnown: currentKnown, EventsDelta: agentDelta, LocalEventsDelta: agentDelta}
		lastGroup := OOMGroupKill{Known: previousKnown, LocalKnown: previousKnown}
		if currentKnown {
			pod.LocalOOMEvents, pod.LocalOOMKillEvents, pod.LocalMaxEvents = current, other, current
			group.Events, group.LocalEvents = current, other
		}
		if previousKnown {
			last.LocalOOMEvents, last.LocalOOMKillEvents, last.LocalMaxEvents = previous, previous, previous
			lastGroup.Events, lastGroup.LocalEvents = previous, previous
		}

		got := WithPodCgroupDeltas(pod, last, hasPrevious)
		gotGroup := WithEventDeltas(MemoryBreakdown{OOMGroupKill: group}, MemoryBreakdown{OOMGroupKill: lastGroup}, hasPrevious).OOMGroupKill
		if err := got.Validate(); err != nil {
			t.Fatalf("Pod cgroup deltas produced an invalid value %#v", got)
		}
		if err := gotGroup.Validate(); err != nil {
			t.Fatalf("oom_group_kill deltas produced an invalid value %#v", gotGroup)
		}
		if got.LocalEventDeltasKnown != currentKnown || gotGroup.DeltasKnown != currentKnown || gotGroup.LocalDeltasKnown != currentKnown {
			t.Fatal("deltas known does not follow the counters being known")
		}
		want := uint64(0)
		if hasPrevious && currentKnown && previousKnown && current >= previous {
			want = current - previous
		}
		if got.LocalOOMEventsDelta != want || gotGroup.EventsDelta != want {
			t.Fatalf("delta %d / %d from %d after %d (previous %t), want %d", got.LocalOOMEventsDelta, gotGroup.EventsDelta, current, previous, hasPrevious, want)
		}
	})
}

// AddMemory copies fields one by one; a field it leaves out is lost in every
// Pod, workload and namespace total.
func TestAddMemoryKeepsZswapAndGroupKillEvidence(t *testing.T) {
	reporting := MemoryBreakdown{Zswap: Zswap{Known: true, PoolBytes: 4, SwappedBytes: 12}, OOMGroupKill: OOMGroupKill{Known: true, Events: 2, DeltasKnown: true, EventsDelta: 1}}
	silent := MemoryBreakdown{TotalBytes: 10}
	sum := SumMemory("pod", []MemoryBreakdown{silent, reporting, reporting})
	if sum.Zswap != (Zswap{Known: true, PoolBytes: 8, SwappedBytes: 24}) {
		t.Fatalf("zswap total = %#v", sum.Zswap)
	}
	if sum.OOMGroupKill != (OOMGroupKill{Known: true, Events: 4, DeltasKnown: true, EventsDelta: 2}) {
		t.Fatalf("oom_group_kill total = %#v", sum.OOMGroupKill)
	}
	if none := SumMemory("pod", []MemoryBreakdown{silent, silent}); none.Zswap != (Zswap{}) || none.OOMGroupKill != (OOMGroupKill{}) {
		t.Fatal("members that report nothing made the total known")
	}
}

package model

import "testing"

func TestSumMemory(t *testing.T) {
	got := SumMemory("pod-a", []MemoryBreakdown{
		{
			TotalBytes:        100,
			AnonBytes:         40,
			FileBytes:         30,
			ActiveFileBytes:   20,
			InactiveFileBytes: 10,
			ShmemBytes:        5,
			SlabBytes:         4,
			KernelBytes:       3,
			DirtyBytes:        2,
			WritebackBytes:    1,
			OOMEvents:         1,
			OOMKillEvents:     2,
			HighEvents:        3,
			MaxEvents:         4,
		},
		{
			TotalBytes:        10,
			AnonBytes:         4,
			FileBytes:         3,
			ActiveFileBytes:   2,
			InactiveFileBytes: 1,
			ShmemBytes:        5,
			SlabBytes:         6,
			KernelBytes:       7,
			DirtyBytes:        8,
			WritebackBytes:    9,
			OOMEvents:         1,
			OOMKillEvents:     1,
			HighEvents:        1,
			MaxEvents:         1,
		},
	})

	if got.Name != "pod-a" {
		t.Fatalf("Name = %q, want pod-a", got.Name)
	}
	if got.TotalBytes != 110 || got.AnonBytes != 44 || got.FileBytes != 33 {
		t.Fatalf("unexpected core totals: %#v", got)
	}
	if got.DirtyWritebackBytes() != 20 {
		t.Fatalf("DirtyWritebackBytes = %d, want 20", got.DirtyWritebackBytes())
	}
	if got.OOMEvents != 2 || got.OOMKillEvents != 3 || got.HighEvents != 4 || got.MaxEvents != 5 {
		t.Fatalf("unexpected event totals: %#v", got)
	}
}

func TestAggregateLimitIsKnownOnlyWhenEveryMemberLimitIs(t *testing.T) {
	limited := MemoryBreakdown{TotalBytes: 60, MaxKnown: true, MaxBytes: 100}
	unread := MemoryBreakdown{TotalBytes: 90}
	unlimited := MemoryBreakdown{TotalBytes: 90, MaxKnown: true, MaxUnlimited: true}
	tests := map[string]struct {
		members   []MemoryBreakdown
		known     bool
		unlimited bool
		limit     uint64
	}{
		"all limited":          {members: []MemoryBreakdown{limited, limited}, known: true, limit: 200},
		"one unread":           {members: []MemoryBreakdown{limited, unread}},
		"unread first":         {members: []MemoryBreakdown{unread, limited}},
		"one unlimited":        {members: []MemoryBreakdown{limited, unlimited}, known: true, unlimited: true},
		"unread and unlimited": {members: []MemoryBreakdown{unlimited, unread}, known: true, unlimited: true},
		"single member":        {members: []MemoryBreakdown{limited}, known: true, limit: 100},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// SumMemory and an incremental fold from an empty accumulator are
			// both used to build Pods, so they must agree.
			folded := MemoryBreakdown{}
			for _, member := range test.members {
				folded = AddMemory(folded, member)
			}
			for path, got := range map[string]MemoryBreakdown{"SumMemory": SumMemory("pod", test.members), "fold": folded} {
				if got.MaxKnown != test.known || got.MaxUnlimited != test.unlimited {
					t.Fatalf("%s: known=%t unlimited=%t, want known=%t unlimited=%t", path, got.MaxKnown, got.MaxUnlimited, test.known, test.unlimited)
				}
				if test.known && !test.unlimited && got.MaxBytes != test.limit {
					t.Fatalf("%s: limit = %d, want %d", path, got.MaxBytes, test.limit)
				}
				if got.HasLimitRisk() != (test.known && !test.unlimited && got.TotalBytes*10 >= got.MaxBytes*9) {
					t.Fatalf("%s: limit risk = %t for %+v", path, got.HasLimitRisk(), got)
				}
			}
		})
	}
}

// Members on kernels with and without memory.events.local can be rolled up
// together. Local counters on one must not hide a recent event on another.
func TestAggregateKeepsRecentEventsFromMembersWithoutLocalEvents(t *testing.T) {
	local := MemoryBreakdown{TotalBytes: 10, LocalEventsKnown: true, LocalEventDeltasKnown: true, EventDeltasKnown: true}
	hierarchical := MemoryBreakdown{TotalBytes: 10, EventDeltasKnown: true, OOMKillEvents: 3, OOMKillEventsDelta: 1, HighEventsDelta: 2}
	for name, members := range map[string][]MemoryBreakdown{
		"local first":        {local, hierarchical},
		"hierarchical first": {hierarchical, local},
		"between locals":     {local, hierarchical, local},
	} {
		t.Run(name, func(t *testing.T) {
			sum := SumMemory("workload", members)
			_, oomKill, high, _ := sum.RecentEventCounts()
			if !sum.HasOOMRisk() || oomKill != 1 || high != 2 {
				t.Fatalf("recent events lost: oom_kill=%d high=%d risk=%t", oomKill, high, sum.HasOOMRisk())
			}
		})
	}
	if sum := SumMemory("pod", []MemoryBreakdown{local, local}); sum.HasOOMRisk() {
		t.Fatalf("quiet local members reported as OOM risk: %+v", sum)
	}
	onlyHierarchical := SumMemory("pod", []MemoryBreakdown{hierarchical, hierarchical})
	if onlyHierarchical.LocalEventsKnown || onlyHierarchical.LocalOOMKillEventsDelta != 0 {
		t.Fatalf("local counters invented for members without them: %+v", onlyHierarchical)
	}
}

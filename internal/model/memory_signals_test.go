package model

import "testing"

func TestAddMemoryAggregatesSecondaryComposition(t *testing.T) {
	got := AddMemory(
		MemoryBreakdown{SlabReclaimableBytes: 1, SlabUnreclaimableBytes: 2, SocketBytes: 3, PageTableBytes: 4, FileMappedBytes: 5, AnonTHPBytes: 6, FileTHPBytes: 7, ShmemTHPBytes: 8},
		MemoryBreakdown{SlabReclaimableBytes: 10, SlabUnreclaimableBytes: 20, SocketBytes: 30, PageTableBytes: 40, FileMappedBytes: 50, AnonTHPBytes: 60, FileTHPBytes: 70, ShmemTHPBytes: 80},
	)
	if got.SlabReclaimableBytes != 11 || got.SlabUnreclaimableBytes != 22 || got.SocketBytes != 33 || got.PageTableBytes != 44 || got.FileMappedBytes != 55 || got.AnonTHPBytes != 66 || got.FileTHPBytes != 77 || got.ShmemTHPBytes != 88 {
		t.Fatalf("unexpected aggregate: %#v", got)
	}
}

func TestWithEventDeltasPrefersLocalEvents(t *testing.T) {
	previous := MemoryBreakdown{
		OOMEvents:          100,
		LocalEventsKnown:   true,
		LocalOOMEvents:     4,
		LocalHighEvents:    7,
		SwapEventsKnown:    true,
		SwapFailEvents:     2,
		LocalMaxEvents:     1,
		LocalOOMKillEvents: 1,
	}
	current := previous
	current.OOMEvents = 150
	current.LocalOOMEvents = 5
	current.LocalHighEvents = 9
	current.SwapFailEvents = 3

	got := WithEventDeltas(current, previous, true)
	oom, _, high, _ := got.RecentEventCounts()
	if oom != 1 || high != 2 {
		t.Fatalf("recent local counts = oom %d high %d, want 1 and 2", oom, high)
	}
	if got.SwapFailEventsDelta != 1 {
		t.Fatalf("SwapFailEventsDelta = %d, want 1", got.SwapFailEventsDelta)
	}
}

func TestFirstSnapshotDoesNotTreatHistoricLocalEventsAsRecent(t *testing.T) {
	got := WithEventDeltas(MemoryBreakdown{
		LocalEventsKnown: true,
		LocalOOMEvents:   8,
	}, MemoryBreakdown{}, false)
	oom, _, _, _ := got.RecentEventCounts()
	if oom != 0 || got.HasOOMRisk() {
		t.Fatalf("first snapshot treated historic local events as recent: %#v", got)
	}
}

func TestWithEventDeltasDerivesReclaimCounters(t *testing.T) {
	previous := MemoryBreakdown{ReclaimCountersKnown: true, WorkingsetRefaultAnon: 2, WorkingsetRefaultFile: 5, PageScan: 10, PageSteal: 8, MajorPageFaults: 3}
	current := MemoryBreakdown{ReclaimCountersKnown: true, WorkingsetRefaultAnon: 4, WorkingsetRefaultFile: 9, PageScan: 20, PageSteal: 15, MajorPageFaults: 4}
	got := WithEventDeltas(current, previous, true)
	if !got.ReclaimDeltasKnown || got.RefaultAnonDelta != 2 || got.RefaultFileDelta != 4 || got.PageScanDelta != 10 || got.PageStealDelta != 7 || got.MajorPageFaultsDelta != 1 {
		t.Fatalf("unexpected reclaim deltas: %#v", got)
	}

	reset := WithEventDeltas(MemoryBreakdown{ReclaimCountersKnown: true, PageScan: 1}, MemoryBreakdown{ReclaimCountersKnown: true, PageScan: 2}, true)
	if reset.ReclaimDeltasKnown {
		t.Fatalf("counter reset reported as a valid delta: %#v", reset)
	}
}

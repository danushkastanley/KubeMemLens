package model

import "testing"

func TestFormatBytes(t *testing.T) {
	tests := map[uint64]string{
		0:          "0 B",
		1024:       "1.00 KiB",
		1048576:    "1.00 MiB",
		1073741824: "1.00 GiB",
	}

	for input, want := range tests {
		if got := FormatBytes(input); got != want {
			t.Fatalf("FormatBytes(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestFormatCompactBytes(t *testing.T) {
	tests := map[uint64]string{
		90 * 1024 * 1024: "90Mi",
		4402341478:       "4.10Gi",
	}

	for input, want := range tests {
		if got := FormatCompactBytes(input); got != want {
			t.Fatalf("FormatCompactBytes(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestMemoryBreakdownRatiosAndHelpers(t *testing.T) {
	breakdown := MemoryBreakdown{
		TotalBytes:     1000,
		AnonBytes:      650,
		FileBytes:      250,
		ShmemBytes:     50,
		SlabBytes:      40,
		KernelBytes:    100,
		DirtyBytes:     10,
		WritebackBytes: 5,
		OOMEvents:      1,
	}

	if breakdown.RSSBytes() != 650 {
		t.Fatalf("RSSBytes = %d, want 650", breakdown.RSSBytes())
	}
	if breakdown.CacheBytes() != 200 {
		t.Fatalf("CacheBytes = %d, want 200", breakdown.CacheBytes())
	}
	if breakdown.FileCacheRatio() != 0.20 {
		t.Fatalf("FileCacheRatio = %f, want 0.20", breakdown.FileCacheRatio())
	}
	if breakdown.DirtyWritebackBytes() != 15 {
		t.Fatalf("DirtyWritebackBytes = %d, want 15", breakdown.DirtyWritebackBytes())
	}
	if breakdown.ResidualBytes() != 100 {
		t.Fatalf("ResidualBytes = %d, want 100", breakdown.ResidualBytes())
	}
	if breakdown.AnonRatio() != 0.65 {
		t.Fatalf("AnonRatio = %f, want 0.65", breakdown.AnonRatio())
	}
	if !breakdown.HasOOMRisk() {
		t.Fatal("HasOOMRisk = false, want true")
	}
}

func TestMemoryBreakdownExclusiveCategoriesFloorAtZero(t *testing.T) {
	breakdown := MemoryBreakdown{
		TotalBytes:  50,
		AnonBytes:   50,
		FileBytes:   40,
		ShmemBytes:  50,
		SlabBytes:   60,
		KernelBytes: 30,
	}

	if breakdown.FileCacheBytes() != 0 {
		t.Fatalf("FileCacheBytes = %d, want 0", breakdown.FileCacheBytes())
	}
	if breakdown.KernelOtherBytes() != 0 {
		t.Fatalf("KernelOtherBytes = %d, want 0", breakdown.KernelOtherBytes())
	}
	if breakdown.ResidualBytes() != 0 {
		t.Fatalf("ResidualBytes = %d, want 0", breakdown.ResidualBytes())
	}
}

func TestMemoryBreakdownResidualUsesNonOverlappingPrimaryBuckets(t *testing.T) {
	breakdown := MemoryBreakdown{
		TotalBytes:  1000,
		AnonBytes:   400,
		FileBytes:   300,
		ShmemBytes:  100,
		SlabBytes:   50,
		KernelBytes: 200,
	}
	if got := breakdown.ResidualBytes(); got != 300 {
		t.Fatalf("ResidualBytes = %d, want 300", got)
	}
}

func TestMemoryBreakdownUsesEventDeltasWhenKnown(t *testing.T) {
	previous := MemoryBreakdown{OOMEvents: 4, OOMKillEvents: 1, HighEvents: 2, MaxEvents: 5}
	unchanged := WithEventDeltas(previous, MemoryBreakdown{}, false)
	if unchanged.HasOOMRisk() {
		t.Fatal("first observed cumulative counters should not be treated as recent OOM risk")
	}

	current := MemoryBreakdown{OOMEvents: 5, OOMKillEvents: 1, HighEvents: 4, MaxEvents: 5}
	withDelta := WithEventDeltas(current, previous, true)
	if !withDelta.HasOOMRisk() {
		t.Fatal("incremented oom counter should be treated as recent OOM risk")
	}
	oom, oomKill, high, maxEvents := withDelta.RecentEventCounts()
	if oom != 1 || oomKill != 0 || high != 2 || maxEvents != 0 {
		t.Fatalf("unexpected deltas: oom=%d oomKill=%d high=%d max=%d", oom, oomKill, high, maxEvents)
	}
}

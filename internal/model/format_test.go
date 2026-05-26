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
		DirtyBytes:     10,
		WritebackBytes: 5,
		OOMEvents:      1,
	}

	if breakdown.RSSBytes() != 650 {
		t.Fatalf("RSSBytes = %d, want 650", breakdown.RSSBytes())
	}
	if breakdown.CacheBytes() != 250 {
		t.Fatalf("CacheBytes = %d, want 250", breakdown.CacheBytes())
	}
	if breakdown.DirtyWritebackBytes() != 15 {
		t.Fatalf("DirtyWritebackBytes = %d, want 15", breakdown.DirtyWritebackBytes())
	}
	if breakdown.AnonRatio() != 0.65 {
		t.Fatalf("AnonRatio = %f, want 0.65", breakdown.AnonRatio())
	}
	if !breakdown.HasOOMRisk() {
		t.Fatal("HasOOMRisk = false, want true")
	}
}

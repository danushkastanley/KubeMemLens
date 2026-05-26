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

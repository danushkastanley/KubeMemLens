package cgroup

import (
	"strings"
	"testing"
)

func TestParseMemoryPressure(t *testing.T) {
	pressure, err := ParseMemoryPressure([]byte(
		"some avg10=1.25 avg60=0.50 avg300=0.10 total=1234\n" +
			"full avg10=0.05 avg60=0.01 avg300=0.00 total=45\n",
	))
	if err != nil {
		t.Fatalf("ParseMemoryPressure returned error: %v", err)
	}
	if pressure.Some.Avg10 != 1.25 || pressure.Some.TotalMicros != 1234 {
		t.Fatalf("unexpected some pressure: %#v", pressure.Some)
	}
	if pressure.Full.Avg10 != 0.05 || pressure.Full.TotalMicros != 45 {
		t.Fatalf("unexpected full pressure: %#v", pressure.Full)
	}
}

func TestParseMemoryPressureRequiresBothClasses(t *testing.T) {
	_, err := ParseMemoryPressure([]byte("some avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"))
	if err == nil || !strings.Contains(err.Error(), "some and full") {
		t.Fatalf("error = %v, want missing pressure class", err)
	}
}

func TestParseDirectoryReadsPressureLimitsSwapAndLocalEvents(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"memory.current":      "900\n",
		"memory.stat":         "anon 500\nfile 200\nshmem 50\nslab 20\nslab_reclaimable 12\nslab_unreclaimable 8\nkernel 40\nsock 7\npagetables 5\nsec_pagetables 3\nfile_mapped 13\nanon_thp 17\nfile_thp 19\nshmem_thp 23\nworkingset_refault_file 7\npgscan 11\npgsteal 9\npgmajfault 3\n",
		"memory.events":       "low 0\nhigh 100\nmax 10\noom 5\noom_kill 4\n",
		"memory.events.local": "low 0\nhigh 2\nmax 1\noom 1\noom_kill 0\n",
		"memory.peak":         "1200\n",
		"memory.min":          "100\n",
		"memory.low":          "200\n",
		"memory.high":         "950\n",
		"memory.max":          "1000\n",
		"memory.swap.current": "64\n",
		"memory.swap.peak":    "128\n",
		"memory.swap.max":     "max\n",
		"memory.swap.events":  "high 1\nmax 2\nfail 3\n",
		"memory.pressure":     "some avg10=1.50 avg60=0.50 avg300=0.10 total=1234\nfull avg10=0.10 avg60=0.05 avg300=0.01 total=42\n",
	}
	for name, data := range files {
		writeFile(t, dir, name, data)
	}

	got, err := ParseDirectory("signals", dir)
	if err != nil {
		t.Fatalf("ParseDirectory returned error: %v", err)
	}
	if !got.PeakKnown || got.PeakBytes != 1200 || !got.MaxKnown || got.MaxBytes != 1000 || got.MaxUnlimited {
		t.Fatalf("unexpected peak/max: %#v", got)
	}
	if !got.SwapMaxKnown || !got.SwapMaxUnlimited || got.SwapCurrentBytes != 64 {
		t.Fatalf("unexpected swap limits: %#v", got)
	}
	if !got.LocalEventsKnown || got.LocalHighEvents != 2 || got.LocalOOMEvents != 1 {
		t.Fatalf("unexpected local events: %#v", got)
	}
	if !got.SwapEventsKnown || got.SwapFailEvents != 3 {
		t.Fatalf("unexpected swap events: %#v", got)
	}
	if !got.PressureKnown || got.PSISomeAvg10 != 1.5 || got.PSIFullTotalMicros != 42 {
		t.Fatalf("unexpected pressure: %#v", got)
	}
	if got.WorkingsetRefaultFile != 7 || got.PageScan != 11 || got.PageSteal != 9 || got.MajorPageFaults != 3 {
		t.Fatalf("unexpected reclaim counters: %#v", got)
	}
	if got.SlabReclaimableBytes != 12 || got.SlabUnreclaimableBytes != 8 || got.SocketBytes != 7 || got.PageTableBytes != 8 {
		t.Fatalf("unexpected secondary kernel detail: %#v", got)
	}
	if got.FileMappedBytes != 13 || got.AnonTHPBytes != 17 || got.FileTHPBytes != 19 || got.ShmemTHPBytes != 23 {
		t.Fatalf("unexpected mapped/THP detail: %#v", got)
	}
	if !got.ReclaimCountersKnown {
		t.Fatalf("reclaim counters were not marked known: %#v", got)
	}
	if !got.HasPressureRisk() || !got.HasLimitRisk() {
		t.Fatalf("expected pressure and limit risk: %#v", got)
	}
}

func TestParseDirectoryAllowsMissingOptionalSignals(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "memory.current", "100\n")
	writeFile(t, dir, "memory.stat", "anon 50\nfile 20\n")

	got, err := ParseDirectory("minimal", dir)
	if err != nil {
		t.Fatalf("ParseDirectory returned error: %v", err)
	}
	if got.PeakKnown || got.MaxKnown || got.PressureKnown || got.LocalEventsKnown || got.SwapCurrentKnown {
		t.Fatalf("optional signals unexpectedly marked known: %#v", got)
	}
}

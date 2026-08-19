package tui

import (
	"math"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestCompositionBarConservesWidthAndSeparatesShmem(t *testing.T) {
	memory := model.MemoryBreakdown{
		TotalBytes: 100,
		AnonBytes:  40,
		FileBytes:  50,
		ShmemBytes: 20,
	}
	bar := compositionBar(memory, 20, true)
	if len(bar) != 20 {
		t.Fatalf("bar width = %d: %q", len(bar), bar)
	}
	if got := strings.Count(bar, "A"); got != 8 {
		t.Fatalf("anon cells = %d: %q", got, bar)
	}
	if got := strings.Count(bar, "F"); got != 6 {
		t.Fatalf("file-cache cells = %d: %q", got, bar)
	}
	if got := strings.Count(bar, "S"); got != 4 {
		t.Fatalf("shmem cells = %d: %q", got, bar)
	}
}

func TestCompositionBarBoundsInconsistentSamples(t *testing.T) {
	memory := model.MemoryBreakdown{
		TotalBytes: 10,
		AnonBytes:  9,
		FileBytes:  9,
		ShmemBytes: 3,
	}
	bar := compositionBar(memory, 24, true)
	if len(bar) != 24 {
		t.Fatalf("bar width = %d: %q", len(bar), bar)
	}
}

func TestCompositionBarHandlesMaximumUintWithoutOverflow(t *testing.T) {
	memory := model.MemoryBreakdown{
		TotalBytes: math.MaxUint64,
		AnonBytes:  math.MaxUint64 / 2,
		FileBytes:  math.MaxUint64 / 4,
	}
	if got := compositionBar(memory, 80, true); len(got) != 80 {
		t.Fatalf("bar width = %d", len(got))
	}
}

func TestLimitLabelStates(t *testing.T) {
	if got := limitLabel(model.MemoryBreakdown{}, 8, true); got != "unknown" {
		t.Fatalf("unknown limit = %q", got)
	}
	if got := limitLabel(model.MemoryBreakdown{MaxKnown: true, MaxUnlimited: true}, 8, true); got != "unlimited" {
		t.Fatalf("unlimited limit = %q", got)
	}
	got := limitLabel(model.MemoryBreakdown{TotalBytes: 90, MaxBytes: 100, MaxKnown: true}, 10, true)
	if got != "#########- 90%" {
		t.Fatalf("90%% limit = %q", got)
	}
}

func TestIncidentLabelDistinguishesBaselineClearAndEvents(t *testing.T) {
	if got := incidentLabel(model.MemoryBreakdown{}); got != "baseline" {
		t.Fatalf("baseline = %q", got)
	}
	if got := incidentLabel(model.MemoryBreakdown{EventDeltasKnown: true}); got != "clear" {
		t.Fatalf("clear = %q", got)
	}
	got := incidentLabel(model.MemoryBreakdown{EventDeltasKnown: true, OOMKillEventsDelta: 1, HighEventsDelta: 2})
	if !strings.Contains(got, "OOM") || !strings.Contains(got, "high 2") {
		t.Fatalf("event label = %q", got)
	}
}

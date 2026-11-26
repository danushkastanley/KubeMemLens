package explain

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestOOMGroupKillIsListedBesideOOMKill(t *testing.T) {
	killed := model.MemoryBreakdown{TotalBytes: 100 * mib, EventDeltasKnown: true, OOMKillEvents: 2, OOMKillEventsDelta: 2,
		OOMGroupKill: model.OOMGroupKill{Known: true, Events: 1, DeltasKnown: true, EventsDelta: 1}}
	signals := strings.Join(Analyze(killed).Signals, "\n")
	if !strings.Contains(signals, "memory.events delta oom_group_kill=1") {
		t.Fatalf("signals do not list the recent group kill:\n%s", signals)
	}

	killed.OOMGroupKill = model.OOMGroupKill{}
	if signals := strings.Join(Analyze(killed).Signals, "\n"); strings.Contains(signals, "oom_group_kill") {
		t.Fatalf("an unreported oom_group_kill was listed:\n%s", signals)
	}
	if _, known := RecentOOMGroupKill(killed); known {
		t.Fatal("RecentOOMGroupKill reports a count the kernel did not provide")
	}
}

func TestOOMGroupKillFollowsTheLocalEventFile(t *testing.T) {
	memory := model.MemoryBreakdown{LocalEventsKnown: true, LocalEventDeltasKnown: true,
		OOMGroupKill: model.OOMGroupKill{Known: true, Events: 7, DeltasKnown: true, EventsDelta: 7}}
	if _, known := RecentOOMGroupKill(memory); known {
		t.Fatal("hierarchical oom_group_kill was used where the local file is read and has no such key")
	}
	memory.OOMGroupKill.LocalKnown, memory.OOMGroupKill.LocalEvents, memory.OOMGroupKill.LocalDeltasKnown = true, 3, true
	if count, known := RecentOOMGroupKill(memory); !known || count != 0 {
		t.Fatalf("RecentOOMGroupKill = %d, %v; want the local delta 0", count, known)
	}
}

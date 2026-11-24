package explain

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

// The OOM signals name the file the counts were read from, and say "delta"
// only when the counts are deltas from that file.
func TestOOMRiskSignalsNameTheSourceOfTheirCounts(t *testing.T) {
	for name, tc := range map[string]struct {
		memory model.MemoryBreakdown
		want   string
	}{
		"local delta": {
			memory: model.MemoryBreakdown{
				LocalEventsKnown: true, LocalEventDeltasKnown: true, LocalOOMKillEventsDelta: 2,
				EventDeltasKnown: true, OOMKillEventsDelta: 7,
			},
			want: "memory.events.local delta oom_kill=2",
		},
		"local cumulative": {
			memory: model.MemoryBreakdown{LocalEventsKnown: true, LocalOOMKillEvents: 3, EventDeltasKnown: true},
			want:   "memory.events.local oom_kill=3",
		},
		"hierarchical delta": {
			memory: model.MemoryBreakdown{EventDeltasKnown: true, OOMKillEventsDelta: 4},
			want:   "memory.events delta oom_kill=4",
		},
		"hierarchical cumulative": {
			memory: model.MemoryBreakdown{OOMKillEvents: 5},
			want:   "memory.events oom_kill=5",
		},
	} {
		t.Run(name, func(t *testing.T) {
			signals := oomRisk(tc.memory).Signals
			if len(signals) == 0 || signals[0] != tc.want {
				t.Fatalf("signals = %q, want the first to be %q", strings.Join(signals, "; "), tc.want)
			}
		})
	}
}

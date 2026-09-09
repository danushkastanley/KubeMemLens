package explain

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func qosFixture() api.ContainerSnapshot {
	now := time.Unix(1000, 0).UTC()
	return api.ContainerSnapshot{CapturedAt: now, Freshness: api.EvidenceFreshnessFresh, DeltaStartedAt: now.Add(-5 * time.Second), DeltaWindowKnown: true,
		Memory:  model.MemoryBreakdown{MinKnown: true, LowKnown: true, HighKnown: true, HighUnlimited: true, MaxKnown: true, MaxBytes: 128 << 20, LocalEventsKnown: true, LocalEventDeltasKnown: true, PressureKnown: true},
		Context: api.ContainerContext{MemoryRequestKnown: true, MemoryRequestBytes: 32 << 20, MemoryLimitKnown: true, MemoryLimitBytes: 128 << 20},
	}
}

func TestMemoryQoSStates(t *testing.T) {
	cases := []struct {
		name       string
		change     func(*api.ContainerSnapshot)
		state      QoSState
		activity   ThrottleActivity
		confidence Confidence
	}{
		{"unlimited is ordinary", func(*api.ContainerSnapshot) {}, QoSObserved, ThrottleNotCrossed, ConfidenceMedium},
		{"configured without crossing", func(c *api.ContainerSnapshot) { c.Memory.HighUnlimited = false; c.Memory.HighBytes = 80 << 20 }, QoSObserved, ThrottleNotCrossed, ConfidenceMedium},
		{"zero throttle below request", func(c *api.ContainerSnapshot) { c.Memory.HighUnlimited = false }, QoSInconsistent, ThrottleNotCrossed, ConfidenceLow},
		{"crossed", func(c *api.ContainerSnapshot) { c.Memory.LocalHighEventsDelta = 3 }, QoSObserved, ThrottleCrossed, ConfidenceMedium},
		{"crossed with stalls", func(c *api.ContainerSnapshot) { c.Memory.LocalHighEventsDelta = 3; c.Memory.PSISomeAvg10 = 2 }, QoSObserved, ThrottleCrossedWithStalls, ConfidenceHigh},
		{"stalls alone", func(c *api.ContainerSnapshot) { c.Memory.PSIFullAvg10 = 0.2 }, QoSObserved, ThrottleStallsOnly, ConfidenceMedium},
		{"missing controls", func(c *api.ContainerSnapshot) { c.Memory.LowKnown = false }, QoSUnavailable, ThrottleNotCrossed, ConfidenceLow},
		{"stale", func(c *api.ContainerSnapshot) { c.Freshness = api.EvidenceFreshnessStale }, QoSStale, ThrottleNotCrossed, ConfidenceLow},
		{"mismatched hard limit", func(c *api.ContainerSnapshot) { c.Memory.MaxBytes = 64 << 20 }, QoSInconsistent, ThrottleNotCrossed, ConfidenceLow},
		{"above hard limit", func(c *api.ContainerSnapshot) { c.Memory.HighUnlimited = false; c.Memory.HighBytes = 256 << 20 }, QoSInconsistent, ThrottleNotCrossed, ConfidenceLow},
		{"resize pending", func(c *api.ContainerSnapshot) {
			c.Context.Resources.Pod.Pending = model.ResizeObservation{State: model.ResizeDeferred, Source: model.ResizePodCondition}
		}, QoSResizing, ThrottleNotCrossed, ConfidenceLow},
		{"observed generation behind", func(c *api.ContainerSnapshot) {
			c.Context.Resources.Pod.Generation = 2
			c.Context.Resources.Pod.ObservedGeneration = 1
		}, QoSResizing, ThrottleNotCrossed, ConfidenceLow},
		{"cumulative is not a delta", func(c *api.ContainerSnapshot) { c.Memory.LocalEventDeltasKnown = false; c.Memory.LocalHighEvents = 999 }, QoSObserved, ThrottleUnreported, ConfidenceMedium},
		{"missing exact window", func(c *api.ContainerSnapshot) { c.Memory.LocalHighEventsDelta = 999; c.DeltaWindowKnown = false }, QoSObserved, ThrottleUnreported, ConfidenceMedium},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := qosFixture()
			tc.change(&c)
			got := InterpretMemoryQoS(c)
			if got.State != tc.state || got.Activity != tc.activity || got.Confidence != tc.confidence {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestMemoryQoSResourceScopeAndPrerequisites(t *testing.T) {
	c := qosFixture()
	c.Context.Resources.Pod.Configured.Limit = model.ResourceValue{Known: true, Bytes: 384 << 20}
	c.Context.MemoryLimitKnown = false
	c.Context.Resources.Applied.Request = model.ResourceValue{Known: true, Bytes: 16 << 20}
	c.Memory.LowBytes = 16 << 20
	c.Memory.HighUnlimited = false
	c.Memory.HighBytes = 80 << 20
	result := InterpretMemoryQoS(c)
	if result.RequestSource != "kubelet-applied" || result.RequestReference.Bytes != 16<<20 || result.PodConfiguredLimit.Bytes != 384<<20 || result.Max.Bytes != 128<<20 || result.State != QoSObserved {
		t.Fatalf("scopes conflated: %+v", result)
	}
	if !strings.Contains(strings.Join(result.Correlations, " "), "memory.low matches") || !strings.Contains(strings.Join(result.Caveats, " "), "Linux 5.9+") {
		t.Fatal(result)
	}
	c.Memory.LocalEventsKnown = false
	c.Memory.EventDeltasKnown = true
	c.Memory.HighEventsDelta = 7
	if got := InterpretMemoryQoS(c); got.EventSource != "memory.events" || got.HighDelta != 7 {
		t.Fatal(got)
	}
	c.Memory.LocalEventsKnown = true
	c.Memory.LocalEventDeltasKnown = false
	if got := InterpretMemoryQoS(c); got.DeltaKnown || got.HighDelta != 0 {
		t.Fatal("hierarchical events replaced unavailable local deltas")
	}
}

func TestCumulativeHighDoesNotClaimRecentPressure(t *testing.T) {
	memory := model.MemoryBreakdown{TotalBytes: 10, LocalEventsKnown: true, LocalHighEvents: 100}
	if result := Analyze(memory); result.Diagnosis == DiagnosisPressure {
		t.Fatalf("cumulative events claimed recent pressure: %+v", result)
	}
}

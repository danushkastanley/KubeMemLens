package recommend

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestMemoryQoSRecommendationsUseObservedEvidence(t *testing.T) {
	now := time.Now().UTC()
	c := api.ContainerSnapshot{CapturedAt: now, DeltaStartedAt: now.Add(-time.Second), DeltaWindowKnown: true, Memory: model.MemoryBreakdown{MinKnown: true, LowKnown: true, HighKnown: true, HighUnlimited: true, MaxKnown: true, MaxUnlimited: true, LocalEventsKnown: true, LocalEventDeltasKnown: true, LocalHighEventsDelta: 2, PressureKnown: true, PSISomeAvg10: 2}}
	pods := []api.PodSnapshot{{Containers: []api.ContainerSnapshot{c, c}}}
	got := ForPodMemoryQoS(pods)
	if len(got) != 1 || len(got[0].Conditions) != 1 || !strings.Contains(got[0].Conditions[0], "exact high-event delta window") {
		t.Fatal(got)
	}
	c.Memory.LocalEventDeltasKnown = false
	c.Memory.LocalHighEvents = 100
	if got := ForPodMemoryQoS([]api.PodSnapshot{{Containers: []api.ContainerSnapshot{c}}}); len(got) != 0 {
		t.Fatal("cumulative counter produced a recent-throttling recommendation")
	}
}

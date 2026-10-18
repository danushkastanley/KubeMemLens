package collector

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

func TestReplicaValuesRequireCompleteOptionalEvidence(t *testing.T) {
	now := time.Now().UTC()
	sample := api.ContainerSnapshot{DeltaStartedAt: now.Add(-5 * time.Second), DeltaWindowKnown: true, Memory: model.MemoryBreakdown{TotalBytes: 100, AnonBytes: 50, FileBytes: 25, ShmemBytes: 5, KernelBytes: 10, SwapCurrentKnown: true, PressureKnown: true, PSISomeAvg10: 2, PSIFullAvg10: .2, MaxKnown: true, MaxBytes: 1000, LocalEventsKnown: true, LocalEventDeltasKnown: true, LocalOOMEventsDelta: 5}}
	for _, missing := range []string{"none", "swap", "pressure", "limit", "counter", "window", "composition"} {
		t.Run(missing, func(t *testing.T) {
			other := sample
			other.Memory.PSISomeAvg10 = 3
			other.Memory.MaxBytes = 500
			switch missing {
			case "swap":
				other.Memory.SwapCurrentKnown = false
			case "pressure":
				other.Memory.PressureKnown = false
			case "limit":
				other.Memory.MaxUnlimited = true
			case "counter":
				other.Memory.LocalEventsKnown = false
				other.Memory.EventDeltasKnown = true
			case "window":
				other.DeltaStartedAt = now.Add(-10 * time.Second)
			case "composition":
				other.Memory.AnonBytes = 0
			}
			values, start := replicaValues([]api.ContainerSnapshot{sample, other}, now)
			if values[replicabaseline.Charge].Number != 200 {
				t.Fatal("charge changed")
			}
			metric := map[string]replicabaseline.Metric{"swap": replicabaseline.Swap, "pressure": replicabaseline.PSISome, "limit": replicabaseline.LimitUsage, "counter": replicabaseline.OOMRate, "window": replicabaseline.OOMRate, "composition": replicabaseline.AnonFraction}[missing]
			if missing != "none" {
				if _, found := values[metric]; found {
					t.Fatal("missing evidence represented as known", metric)
				}
				return
			}
			if values[replicabaseline.PSISome].Number != 3 || values[replicabaseline.LimitUsage].Number != .2 || values[replicabaseline.OOMRate].Number != 2 || !start.Equal(sample.DeltaStartedAt) {
				t.Fatalf("incorrect aggregation: %+v", values)
			}
		})
	}
}

func TestReplicaValuesDoNotClampInvalidComposition(t *testing.T) {
	sample := api.ContainerSnapshot{Memory: model.MemoryBreakdown{TotalBytes: 10, AnonBytes: 11, FileBytes: 20, ShmemBytes: 1}}
	values, _ := replicaValues([]api.ContainerSnapshot{sample}, time.Now())
	for _, metric := range []replicabaseline.Metric{replicabaseline.AnonFraction, replicabaseline.FileFraction, replicabaseline.KernelFraction} {
		if _, found := values[metric]; found {
			t.Fatal("invalid or absent component compared", metric)
		}
	}
	if values[replicabaseline.Charge].Number != 10 {
		t.Fatal("valid charge lost")
	}
}

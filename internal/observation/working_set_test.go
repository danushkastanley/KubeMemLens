package observation

import (
	"math"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func sample(bytes uint64, at time.Time) WorkingSet {
	return WorkingSet{Bytes: &bytes, Availability: capability.Available, Coverage: Coverage{Reported: 1, Expected: 1, Unit: capability.ContainerScope},
		Evidence: capability.Envelope{Source: capability.KubernetesMetrics, APIVersion: "metrics.k8s.io/v1", CapturedAt: at,
			Freshness: capability.Fresh, Completeness: capability.Complete, Stability: capability.Stable}}
}

func TestWorkingSetSumKeepsMissingSeparateFromZero(t *testing.T) {
	now := time.Now().UTC()
	missing := WorkingSet{Coverage: Coverage{Expected: 1, Unit: capability.ContainerScope}}
	if result := SumWorkingSets([]WorkingSet{missing}, capability.PodScope); result.Bytes != nil || result.Coverage.Expected != 1 || result.Evidence.Completeness != capability.Partial {
		t.Fatalf("%+v", result)
	}
	result := SumWorkingSets([]WorkingSet{sample(0, now), missing}, capability.PodScope)
	if result.Bytes == nil || *result.Bytes != 0 || result.Coverage.Reported != 1 || result.Coverage.Expected != 2 || result.Reason != IncompleteMetrics {
		t.Fatalf("%+v", result)
	}
}

func TestWorkingSetSumPreservesTimeRangeAndStaleEvidence(t *testing.T) {
	now := time.Now().UTC()
	stale := sample(20, now.Add(-time.Minute))
	stale.Evidence.Freshness = capability.Stale
	for _, samples := range [][]WorkingSet{{sample(10, now), stale}, {stale, sample(10, now)}} {
		result := SumWorkingSets(samples, capability.WorkloadScope)
		if result.Bytes == nil || *result.Bytes != 30 || result.Evidence.Freshness != capability.Stale || result.Evidence.CapturedAt != stale.Evidence.CapturedAt || result.LatestSampleAt != now || len(result.Evidence.Caveats) != 1 {
			t.Fatalf("%+v", result)
		}
	}
}

func TestWorkingSetSumRejectsOverflowAndIncompatibleSources(t *testing.T) {
	now := time.Now().UTC()
	other := sample(1, now)
	other.Evidence.Source = capability.Cgroup
	for _, values := range [][]WorkingSet{{sample(math.MaxInt64, now), sample(1, now)}, {sample(1, now), other}} {
		result := SumWorkingSets(values, capability.NamespaceScope)
		if result.Bytes != nil || result.Availability != capability.Unavailable {
			t.Fatalf("%+v", result)
		}
	}
}

func TestMissingWorkingSetAggregateRetainsPermissionReason(t *testing.T) {
	denied := WorkingSet{Availability: capability.Forbidden, Reason: capability.AccessDenied, Coverage: Coverage{Expected: 1, Unit: capability.ContainerScope}}
	result := SumWorkingSets([]WorkingSet{denied, denied}, capability.PodScope)
	if result.Bytes != nil || result.Availability != capability.Forbidden || result.Reason != capability.AccessDenied {
		t.Fatalf("%+v", result)
	}
}
